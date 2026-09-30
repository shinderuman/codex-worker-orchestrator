package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
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
	active, err := repositoryharness.RuntimeActive(cfg.RepoRoot, st)
	if err != nil {
		return err
	}
	if !active {
		return executeLegacyStateBacked(cmd, owner, cfg, st, rf, stdout)
	}

	controllerStore, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	controllerLock, err := AcquireRepoLock(controllerStore.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = controllerLock.Close() }()

	legacyLock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = legacyLock.Close() }()
	if err := admitParentCommand(cmd, st); err != nil {
		return err
	}

	admission, err := admitControllerMutation(cmd, cfg, st, controllerStore)
	if err != nil {
		return err
	}
	operationErr := executeControllerAdmittedCommand(cmd, owner, cfg, st, rf, stdout)
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
	if _, provenanceErr := controllerStore.RecordMutation(admission, controllerCommandIdentity(cmd), outcome, after); provenanceErr != nil {
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

func executeLegacyStateBacked(
	cmd Command,
	owner commandDispatchOwner,
	cfg config.AppConfig,
	st *state.StateStore,
	rf RunnerFactory,
	stdout io.Writer,
) error {
	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := admitParentCommand(cmd, st); err != nil {
		return err
	}
	return executeControllerAdmittedCommand(cmd, owner, cfg, st, rf, stdout)
}

func admitControllerMutation(
	cmd Command,
	cfg config.AppConfig,
	st *state.StateStore,
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
	pinnedTask := st.ReadOr("active-task", "")
	if cmd.Mode == ModeNewTask {
		pinnedTask = ""
	}
	task, err := controller.ResolveSemanticTaskRef(cfg.RepoRoot, pinnedTask)
	if err != nil {
		return controller.Admission{}, err
	}
	head, err := controllerStore.LoadHead()
	if err != nil {
		return controller.Admission{}, err
	}
	if head.LiveLeaseID == "" {
		return controllerStore.BootstrapExecution(task, workspace, snapshot)
	}
	if cmd.Mode == ModeNewTask {
		return controllerStore.RotateExecution(task, workspace, snapshot, "new-task")
	}
	return controllerStore.AdmitMutationOrFailClosed(task, workspace, snapshot)
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
