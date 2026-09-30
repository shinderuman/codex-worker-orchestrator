package parentactioncmd

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

func parentActionNeedsControllerGuard(descriptor parentActionCommandDescriptor, execution parentActionExecutionKind) bool {
	switch execution {
	case parentActionExecutionSessionRotation,
		parentActionExecutionLifecycle,
		parentActionExecutionComplete,
		parentActionExecutionInstall,
		parentActionExecutionDefectRegistration,
		parentActionExecutionImprovementDisposition:
		return true
	case parentActionExecutionGitEvidence:
		return descriptor.Action == "push-binding"
	default:
		return false
	}
}

func executeControllerGuardedParentMutation(
	cfg config.AppConfig,
	descriptor parentActionCommandDescriptor,
	execution parentActionExecutionKind,
	operation func() error,
) error {
	controllerStore, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(controllerStore.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	admission, err := admitParentControllerMutation(cfg, controllerStore)
	if err != nil {
		return err
	}
	operationErr := operation()
	return finalizeParentControllerMutation(cfg, descriptor, execution, controllerStore, admission, operationErr)
}

func admitParentControllerMutation(
	cfg config.AppConfig,
	controllerStore *controller.Store,
) (controller.Admission, error) {
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, controllerStore.Identity())
	if err != nil {
		return controller.Admission{}, err
	}
	before, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return controller.Admission{}, err
	}
	head, err := controllerStore.LoadHead()
	if err != nil {
		return controller.Admission{}, err
	}
	if head.LiveLeaseID == "" {
		authority, authorityErr := controller.ResolveCommittedTaskAuthority(controllerStore.Identity().PrimaryRoot)
		if authorityErr != nil {
			return controller.Admission{}, authorityErr
		}
		return controllerStore.BootstrapExecution(authority.Task, workspace, before)
	}
	if head.ExecutionTaskRef == nil {
		return controller.Admission{}, fmt.Errorf("repository controller has no execution task authority")
	}
	return controllerStore.AdmitMutationOrFailClosed(*head.ExecutionTaskRef, workspace, before)
}

func finalizeParentControllerMutation(
	cfg config.AppConfig,
	descriptor parentActionCommandDescriptor,
	execution parentActionExecutionKind,
	controllerStore *controller.Store,
	admission controller.Admission,
	operationErr error,
) error {
	after, snapshotErr := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		_, failErr := controllerStore.FailClosed(
			"workspace snapshot became unreadable after admitted parent mutation",
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
	command := fmt.Sprintf("glm-parent-action:%s:%d", descriptor.Action, execution)
	if _, provenanceErr := controllerStore.RecordGuardedMutation(admission, command, outcome, after); provenanceErr != nil {
		_, failErr := controllerStore.FailClosed(
			"admitted parent mutation could not commit repository provenance",
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
