package parentactioncmd

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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
	st := state.AttachStateStore(cfg)
	active, err := repositoryharness.RuntimeActive(cfg.RepoRoot, st)
	if err != nil {
		return err
	}
	if !active {
		return operation()
	}
	controllerStore, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(controllerStore.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, controllerStore.Identity())
	if err != nil {
		return err
	}
	before, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	task, err := controller.ResolveSemanticTaskRef(cfg.RepoRoot, st.ReadOr("active-task", ""))
	if err != nil {
		return err
	}
	head, err := controllerStore.LoadHead()
	if err != nil {
		return err
	}
	var admission controller.Admission
	if head.LiveLeaseID == "" {
		admission, err = controllerStore.BootstrapExecution(task, workspace, before)
	} else {
		admission, err = controllerStore.AdmitMutationOrFailClosed(task, workspace, before)
	}
	if err != nil {
		return err
	}

	operationErr := operation()
	after, snapshotErr := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		_, failErr := controllerStore.FailClosed(
			"workspace snapshot became unreadable after admitted parent mutation",
			"",
			workspace,
			before,
			controller.WorkspaceSnapshot{},
			nil,
		)
		return errors.Join(operationErr, snapshotErr, failErr)
	}
	if operationErr != nil && after.ID == before.ID {
		return operationErr
	}
	outcome := "success"
	if operationErr != nil {
		outcome = "error"
	}
	command := fmt.Sprintf("glm-parent-action:%s:%d", descriptor.Action, execution)
	if _, provenanceErr := controllerStore.RecordMutation(admission, command, outcome, after); provenanceErr != nil {
		_, failErr := controllerStore.FailClosed(
			"admitted parent mutation could not commit repository provenance",
			"",
			workspace,
			before,
			after,
			nil,
		)
		return errors.Join(operationErr, provenanceErr, failErr)
	}
	return operationErr
}
