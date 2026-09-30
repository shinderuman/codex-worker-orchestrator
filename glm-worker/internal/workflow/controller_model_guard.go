package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type controllerModelCallGuard struct {
	store     *controller.Store
	admission controller.Admission
	before    controller.WorkspaceSnapshot
	active    bool
}

func (w *Workflow) admitControllerModelCall(checkpoint state.ResumeCheckpoint) (controllerModelCallGuard, error) {
	if checkpoint.ReadOnly {
		return controllerModelCallGuard{}, nil
	}
	decision, err := repositoryharness.Evaluate(w.config.RepoRoot)
	if err != nil {
		return controllerModelCallGuard{}, fmt.Errorf("evaluate repository controller applicability: %w", err)
	}
	if !decision.Active {
		return controllerModelCallGuard{}, nil
	}
	store, err := controller.Open(w.config)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	workspace, err := controller.ResolveWorkspaceIdentity(w.config.RepoRoot, store.Identity())
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	before, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	admission, err := w.resolveControllerModelAdmission(store, head, workspace, before)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	return controllerModelCallGuard{store: store, admission: admission, before: before, active: true}, nil
}

func (w *Workflow) resolveControllerModelAdmission(
	store *controller.Store,
	head controller.RepositoryControllerHead,
	workspace controller.WorkspaceIdentity,
	before controller.WorkspaceSnapshot,
) (controller.Admission, error) {
	if head.LiveAttemptID == "" && head.LiveLeaseID == "" {
		authority, err := controller.ResolveCommittedTaskAuthority(w.config.RepoRoot)
		if err != nil {
			return controller.Admission{}, err
		}
		return store.BootstrapExecution(authority.Task, workspace, before)
	}
	if head.ExecutionTaskRef == nil {
		return controller.Admission{}, fmt.Errorf("repository controller has live runtime authority without execution task")
	}
	return store.AdmitMutationOrFailClosed(*head.ExecutionTaskRef, workspace, before)
}

func (w *Workflow) captureControllerModelCallAfter(guard controllerModelCallGuard) (controller.WorkspaceSnapshot, error) {
	if !guard.active {
		return controller.WorkspaceSnapshot{}, nil
	}
	after, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
	if err == nil {
		return after, nil
	}
	if failErr := guard.store.FailClosedAdmission(guard.admission, "model call post-state could not be captured", controller.WorkspaceSnapshot{}); failErr != nil {
		return controller.WorkspaceSnapshot{}, fmt.Errorf("capture model call controller snapshot: %w; fail-close: %v", err, failErr)
	}
	return controller.WorkspaceSnapshot{}, fmt.Errorf("capture model call controller snapshot: %w", err)
}

func (w *Workflow) commitControllerModelCall(
	guard controllerModelCallGuard,
	after controller.WorkspaceSnapshot,
	checkpoint state.ResumeCheckpoint,
	runErr error,
) error {
	if !guard.active || after.ID == guard.before.ID {
		return nil
	}
	outcome := "success"
	if runErr != nil {
		outcome = "error"
	}
	command := "model:" + string(checkpoint.Role) + ":" + checkpoint.Phase
	_, err := guard.store.RecordAdmittedMutation(guard.admission, command, outcome, after)
	return err
}

func (w *Workflow) failControllerModelCall(
	guard controllerModelCallGuard,
	after controller.WorkspaceSnapshot,
	reason string,
) error {
	if !guard.active || after.ID == guard.before.ID {
		return nil
	}
	if reason == "" {
		reason = "model call violated repository guard"
	}
	return guard.store.FailClosedAdmission(guard.admission, reason, after)
}
