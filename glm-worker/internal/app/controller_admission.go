package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeControllerGuardedStateBacked(
	cmd Command,
	owner commandDispatchOwner,
	cfg config.AppConfig,
	st *state.StateStore,
	rf RunnerFactory,
	stdout io.Writer,
) error {
	controllerStore, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	controllerLock, err := AcquireRepoLock(controllerStore.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = controllerLock.Close() }()

	if err := preflightControllerWorkspace(cmd, cfg, controllerStore); err != nil {
		return err
	}
	admission, err := admitControllerMutation(cmd, cfg, controllerStore)
	if err != nil {
		return err
	}
	operationErr := executeControllerAdmittedCommand(cmd, owner, cfg, st, rf, stdout)
	return finalizeControllerGuardedMutation(cmd, cfg, controllerStore, admission, operationErr)
}

func finalizeControllerGuardedMutation(
	cmd Command,
	cfg config.AppConfig,
	controllerStore *controller.Store,
	admission controller.Admission,
	operationErr error,
) error {
	after, snapshotErr := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		_, failErr := controllerStore.FailClosed(
			"workspace snapshot became unreadable after admitted mutation",
			"",
			admission.Workspace,
			admission.Snapshot,
			controller.WorkspaceSnapshot{},
			nil,
		)
		return errors.Join(operationErr, snapshotErr, failErr)
	}
	if operationErr != nil && after.ID == admission.Snapshot.ID {
		return operationErr
	}
	outcome := "success"
	if operationErr != nil {
		outcome = "error"
	}
	if _, provenanceErr := controllerStore.RecordGuardedMutation(admission, controllerCommandIdentity(cmd), outcome, after); provenanceErr != nil {
		_, failErr := controllerStore.FailClosed(
			"admitted mutation could not commit repository provenance",
			"",
			admission.Workspace,
			admission.Snapshot,
			after,
			nil,
		)
		return errors.Join(operationErr, provenanceErr, failErr)
	}
	return operationErr
}

func preflightControllerWorkspace(cmd Command, cfg config.AppConfig, controllerStore *controller.Store) error {
	if cmd.Mode != ModeNewTask {
		return nil
	}
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, controllerStore.Identity())
	if err != nil {
		return err
	}
	if workspace.Root != controllerStore.Identity().PrimaryRoot {
		return fmt.Errorf("new task requires the verified primary worktree")
	}
	return nil
}

func admitControllerMutation(
	cmd Command,
	cfg config.AppConfig,
	controllerStore *controller.Store,
) (controller.Admission, error) {
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, controllerStore.Identity())
	if err != nil {
		return controller.Admission{}, err
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return controller.Admission{}, err
	}
	head, err := controllerStore.LoadHead()
	if err != nil {
		return controller.Admission{}, err
	}
	if head.LiveLeaseID == "" || cmd.Mode == ModeNewTask {
		authority, authorityErr := controller.ResolveCommittedTaskAuthority(controllerStore.Identity().PrimaryRoot)
		if authorityErr != nil {
			return controller.Admission{}, authorityErr
		}
		if head.LiveLeaseID == "" {
			return controllerStore.BootstrapExecution(authority.Task, workspace, snapshot)
		}
		return controllerStore.RotateExecution(authority.Task, workspace, snapshot, "new-task")
	}
	if head.ExecutionTaskRef == nil {
		return controller.Admission{}, fmt.Errorf("repository controller has no execution task authority")
	}
	return controllerStore.AdmitMutationOrFailClosed(*head.ExecutionTaskRef, workspace, snapshot)
}

func executeControllerAdmittedCommand(
	cmd Command,
	owner commandDispatchOwner,
	cfg config.AppConfig,
	st *state.StateStore,
	rf RunnerFactory,
	stdout io.Writer,
) error {
	switch owner {
	case dispatchLockedMutation:
		return executeLockedMutation(cmd, cfg, st, stdout)
	case dispatchWorkflow:
		return executeWorkflow(cmd, cfg, st, rf, stdout)
	default:
		return fmt.Errorf("unsupported state-backed dispatch owner: %d", owner)
	}
}

func controllerCommandIdentity(cmd Command) string {
	return fmt.Sprintf("glm-worker-mode:%d", cmd.Mode)
}
