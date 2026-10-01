package workflow

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
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
	exists, err := controller.Exists(w.config)
	if err != nil {
		return controllerModelCallGuard{}, fmt.Errorf("inspect repository controller activation: %w", err)
	}
	if !exists {
		return controllerModelCallGuard{}, nil
	}
	store, err := controller.Open(w.config)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	if controller.IsPristine(head) {
		return controllerModelCallGuard{}, nil
	}
	workspace, err := controller.ResolveWorkspaceIdentity(w.config.RepoRoot, store.Identity())
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	before, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	authority, err := controller.MutationAuthorityFromHead(head)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	admission, err := store.AdmitMutationOrFailClosed(authority, workspace, before)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	admission, err = store.BindModelCall(admission)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	return controllerModelCallGuard{store: store, admission: admission, before: before, active: true}, nil
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
		return controller.WorkspaceSnapshot{}, errors.Join(fmt.Errorf("capture model call controller snapshot: %w", err), fmt.Errorf("controller fail-close: %w", failErr))
	}
	return controller.WorkspaceSnapshot{}, fmt.Errorf("capture model call controller snapshot: %w", err)
}

func (*Workflow) commitControllerModelCall(
	guard controllerModelCallGuard,
	after controller.WorkspaceSnapshot,
	checkpoint state.ResumeCheckpoint,
	runErr error,
) error {
	if !guard.active {
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

func (*Workflow) failControllerModelCall(
	guard controllerModelCallGuard,
	after controller.WorkspaceSnapshot,
	reason string,
) error {
	if !guard.active {
		return nil
	}
	if reason == "" {
		reason = "model call violated repository guard"
	}
	return guard.store.FailClosedAdmission(guard.admission, reason, after)
}
