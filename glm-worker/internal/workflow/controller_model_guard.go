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

func (w *Workflow) admitCanonicalWorkflowMutation() error {
	admission, err := w.canonicalWorkflowAdmission(w.state.ReadOr("last-request", ""))
	if err != nil {
		return err
	}
	if err := w.bindCanonicalWorkflowAttempt(admission); err != nil {
		return err
	}
	w.canonicalAdmission = &admission
	return nil
}

func (w *Workflow) admitCanonicalNewTaskMutation(request string) error {
	admission, err := w.canonicalWorkflowAdmission(request)
	if err != nil {
		return err
	}
	w.canonicalAdmission = &admission
	return nil
}

func (w *Workflow) canonicalWorkflowAdmission(request string) (controller.Admission, error) {
	if w.state.TaskStatus() != state.TaskStatusNone {
		exists, err := controller.Exists(w.config)
		if err != nil {
			return controller.Admission{}, err
		}
		if !exists {
			return controller.Admission{}, fmt.Errorf("existing workflow requires canonical controller execution authority")
		}
	}
	admission, err := controller.ActivateRequest(w.config, request)
	if err != nil {
		return controller.Admission{}, err
	}
	if required, err := w.requiredTaskAuthoritySync(admission.Head); err != nil {
		return controller.Admission{}, err
	} else if required != nil {
		return controller.Admission{}, required
	}
	return admission, nil
}

func (w *Workflow) requiredTaskAuthoritySync(head controller.RepositoryControllerHead) (*controller.TaskAuthoritySyncRequiredError, error) {
	store, err := controller.Open(w.config)
	if err != nil {
		return nil, err
	}
	return store.RequiredTaskAuthoritySync(head)
}

func (w *Workflow) admitControllerModelCall(checkpoint state.ResumeCheckpoint) (controllerModelCallGuard, error) {
	store, head, active, err := w.liveControllerStore()
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	if active {
		if err := w.validateCanonicalModelAuthority(store, head, checkpoint); err != nil {
			return controllerModelCallGuard{}, err
		}
	}
	if checkpoint.ReadOnly {
		if active && head.ExecutionTaskRef != nil && head.ExecutionTaskRef.RequestID != "" {
			return w.admitControllerSource(store, head)
		}
		return controllerModelCallGuard{}, nil
	}
	if !active {
		return controllerModelCallGuard{}, fmt.Errorf("mutating model call requires canonical controller execution authority")
	}
	guard, err := w.admitControllerSource(store, head)
	if err != nil {
		return controllerModelCallGuard{}, err
	}
	guard.admission, err = store.BindModelCall(guard.admission)
	return guard, err
}

func (w *Workflow) admitControllerSource(store *controller.Store, head controller.RepositoryControllerHead) (controllerModelCallGuard, error) {
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
	return controllerModelCallGuard{store: store, admission: admission, before: before, active: true}, nil
}

func (w *Workflow) liveControllerStore() (*controller.Store, controller.RepositoryControllerHead, bool, error) {
	present, err := controller.RepositoryPresent(w.config.RepoRoot)
	if err != nil {
		return nil, controller.RepositoryControllerHead{}, false, fmt.Errorf("inspect repository controller applicability: %w", err)
	}
	if !present {
		return nil, controller.RepositoryControllerHead{}, false, nil
	}
	exists, err := controller.Exists(w.config)
	if err != nil {
		return nil, controller.RepositoryControllerHead{}, false, fmt.Errorf("inspect repository controller activation: %w", err)
	}
	if !exists {
		return nil, controller.RepositoryControllerHead{}, false, nil
	}
	store, err := controller.Open(w.config)
	if err != nil {
		return nil, controller.RepositoryControllerHead{}, false, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return nil, controller.RepositoryControllerHead{}, false, err
	}
	if controller.IsPristine(head) {
		return nil, controller.RepositoryControllerHead{}, false, nil
	}
	return store, head, true, nil
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
	if checkpoint.ReadOnly {
		if after == guard.before {
			return nil
		}
		cause := fmt.Errorf("read-only request model call changed repository source")
		if err := guard.store.FailClosedAdmission(guard.admission, cause.Error(), after); err != nil {
			return errors.Join(cause, err)
		}
		return cause
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

func (w *Workflow) validateCanonicalModelAuthority(store *controller.Store, head controller.RepositoryControllerHead, checkpoint state.ResumeCheckpoint) error {
	if err := w.validateModelRequest(store, head, checkpoint); err != nil {
		return err
	}
	required, err := store.RequiredTaskAuthoritySync(head)
	if err != nil {
		return err
	}
	if required != nil {
		return required
	}
	return nil
}

func (w *Workflow) validateModelRequest(store *controller.Store, head controller.RepositoryControllerHead, checkpoint state.ResumeCheckpoint) error {
	if head.ExecutionTaskRef == nil || head.ExecutionTaskRef.RequestID == "" {
		return nil
	}
	saved := w.state.ReadOr("last-request", "")
	if checkpoint.Request != "" && checkpoint.Request != saved {
		return fmt.Errorf("checkpoint request differs from canonical runtime request")
	}
	return store.ValidateRequestTask(*head.ExecutionTaskRef, saved)
}
