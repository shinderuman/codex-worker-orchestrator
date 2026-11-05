package workflow

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ControllerExecutionCommand struct {
	RuntimeDisposition     *controller.RuntimeDispositionInput      `json:"runtime_disposition,omitempty"`
	SourceSnapshotID       string                                   `json:"source_snapshot_id,omitempty"`
	CandidateID            string                                   `json:"candidate_id,omitempty"`
	Action                 string                                   `json:"action"`
	ExpectedGeneration     uint64                                   `json:"expected_generation"`
	EpisodeID              string                                   `json:"episode_id,omitempty"`
	EpisodeRevision        uint64                                   `json:"episode_revision,omitempty"`
	SuspensionID           string                                   `json:"suspension_id,omitempty"`
	WorkspaceID            string                                   `json:"workspace_id,omitempty"`
	SealRef                *controller.EvidenceObjectRef            `json:"seal_ref,omitempty"`
	TransitionID           string                                   `json:"transition_id,omitempty"`
	UserTermination        *controller.UserTaskTransitionInput      `json:"user_termination,omitempty"`
	ExecutionTermination   *controller.UserExecutionTransitionInput `json:"execution_termination,omitempty"`
	RevisedTask            *controller.SemanticTaskRef              `json:"revised_task,omitempty"`
	AcceptedResultRecovery *AcceptedResultRuleRecoveryInput         `json:"accepted_result_recovery,omitempty"`
}

func ExecuteControllerExecution(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if err := store.ValidateConfigScope(cfg); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if command.Action == "recover" {
		if command.TransitionID == "" {
			return controller.ExecutionOperationResult{}, fmt.Errorf("execution recovery requires transition identity")
		}
		return executeControllerExecutionRecovery(cfg, store, command.TransitionID)
	}
	head, err := store.LoadHead()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if command.ExpectedGeneration != head.ControllerGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution command generation is stale")
	}
	if command.Action == "repair-reentry-review" || command.Action == "update-reentry-review" {
		return executeControllerReentryReviewRepair(cfg, store, command.SourceSnapshotID, command.RevisedTask)
	}
	return executeControllerExecutionAction(cfg, store, command)
}

func executeControllerExecutionAction(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	switch command.Action {
	case "dispose-runtime":
		return executeControllerRuntimeDisposition(store, command)
	case "recover-runtime-start":
		return executeControllerRuntimeStartRecovery(cfg, store)
	case "recover-accepted-result-rule-activation":
		return executeControllerAcceptedResultRecovery(cfg, store, command.AcceptedResultRecovery)
	case "user-terminate-task":
		return executeControllerUserTermination(store, command)
	case "user-terminate-execution":
		return executeControllerUserExecutionTermination(store, command)
	case "suspend":
		return executeControllerSuspend(cfg, store, command)
	case "materialize":
		return executeControllerMaterialize(cfg, store, command)
	case controller.PublicationOperationParentOnlyMaterialize:
		return store.AdmitParentOnlyExecution(controller.ParentOnlyMaterializeInput{ExpectedGeneration: command.ExpectedGeneration})
	case "cleanup":
		return executeControllerCleanup(store, command)
	case "gc-suspension":
		return store.GarbageCollectSuspension(command.ExpectedGeneration, command.SuspensionID)
	default:
		return executeControllerParentSourceAction(cfg, store, command)
	}
}

func executeControllerParentSourceAction(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	switch command.Action {
	case "preview-parent-source":
		return executeControllerParentSourcePreview(store)
	case "rebind-parent-source":
		return store.RebindParentSourceCandidate(controller.PublicationInput{ExpectedGeneration: command.ExpectedGeneration, CandidateID: command.CandidateID}, command.SourceSnapshotID)
	case "sync-task-authority":
		return executeControllerTaskAuthoritySync(cfg, store, command)
	case "prepare-reentry-review":
		return executeControllerReentryReview(cfg, store)
	default:
		return controller.ExecutionOperationResult{}, fmt.Errorf("unsupported controller execution action %q", command.Action)
	}
}

func executeControllerUserExecutionTermination(store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.ExecutionTermination == nil || command.ExecutionTermination.ExpectedGeneration != command.ExpectedGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution termination requires exact transition authority")
	}
	return store.TerminateExecutionAndSelectSuccessor(*command.ExecutionTermination)
}

func executeControllerParentSourcePreview(store *controller.Store) (controller.ExecutionOperationResult, error) {
	snapshot, err := controller.CaptureWorkspaceSnapshot(store.Identity().PrimaryRoot)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head, SourceSnapshot: &snapshot}, err
}

func executeControllerUserTermination(store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.UserTermination == nil || command.UserTermination.ExpectedGeneration != command.ExpectedGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("user termination requires exact transition authority")
	}
	return store.TerminateTaskAndSelectSuccessor(*command.UserTermination)
}

func executeControllerReentryReview(cfg config.AppConfig, store *controller.Store) (controller.ExecutionOperationResult, error) {
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if err := wf.PrepareReenteredReview(); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head}, err
}

func executeControllerCleanup(store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.SealRef == nil || command.WorkspaceID == "" {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution cleanup requires sealed workspace identity")
	}
	return store.CleanupExecution(controller.CleanupExecutionInput{ExpectedGeneration: command.ExpectedGeneration, WorkspaceID: command.WorkspaceID, SealRef: *command.SealRef})
}

func executeControllerAcceptedResultRecovery(cfg config.AppConfig, store *controller.Store, input *AcceptedResultRuleRecoveryInput) (controller.ExecutionOperationResult, error) {
	if input == nil {
		return controller.ExecutionOperationResult{}, fmt.Errorf("accepted-result recovery requires exact source identity")
	}
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if err := wf.RecoverAcceptedResultRuleActivation(*input); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head}, err
}

func executeControllerRuntimeStartRecovery(cfg config.AppConfig, store *controller.Store) (controller.ExecutionOperationResult, error) {
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if err := wf.RecoverUnstartedMilestone(); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head}, err
}

func executeControllerMaterialize(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	result, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: command.ExpectedGeneration, EpisodeID: command.EpisodeID, EpisodeRevision: command.EpisodeRevision, SuspensionID: command.SuspensionID})
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := completeMaterializedRuntime(cfg, store, result); err != nil {
		return result, err
	}
	return result, nil
}

func completeMaterializedRuntime(cfg config.AppConfig, store *controller.Store, result controller.ExecutionOperationResult) error {
	if result.Admission == nil {
		return fmt.Errorf("materialized execution transition %s lacks admitted runtime identity", result.TransitionID)
	}
	if result.Admission.Attempt.ExecutionAuthority == controller.ExecutionAuthorityParentOnly {
		return nil
	}
	if err := reenterMaterializedRuntimeCompletion(cfg, result); err != nil {
		return fmt.Errorf("materialized execution transition %s runtime reentry failed: %w", result.TransitionID, err)
	}
	if !result.ParentSourceReview {
		return nil
	}
	if err := completeMaterializedReview(cfg, store, result); err != nil {
		return fmt.Errorf("materialized execution transition %s review completion failed: %w", result.TransitionID, err)
	}
	return nil
}

func completeMaterializedReview(cfg config.AppConfig, store *controller.Store, result controller.ExecutionOperationResult) error {
	cfg.RepoRoot = result.Admission.Workspace.Root
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return err
	}
	retained, err := state.AttachStateStore(runtimeCfg).ReenteredReviewerStopRetained()
	if err != nil {
		return err
	}
	if retained {
		return verifyRetainedMaterializedReview(runtimeCfg, result.Admission.Attempt)
	}
	// A source-bound, independently authenticated HIGH Review may survive a
	// metadata-only fast-forward without another model call. The canonical
	// workflow attempt binding and parent evidence gates remain mandatory.
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if review, err := wf.state.CurrentParentReviewBinding(); err == nil && review != nil && review.SourceEquivalence != nil {
		if err := wf.admitCanonicalReenteredReview(); err != nil {
			return err
		}
		admitted, err := admittedEquivalentReview(wf.state, result.Admission.Attempt)
		if err != nil || !admitted {
			return fmt.Errorf("equivalent Review failed current source admission: %v", err)
		}
		return nil
	}
	if _, err := executeControllerReentryReview(cfg, store); err != nil {
		return fmt.Errorf("materialized execution transition %s review preparation failed: %w", result.TransitionID, err)
	}
	return nil
}

func verifyRetainedMaterializedReview(cfg config.AppConfig, attempt controller.AttemptRecord) error {
	runtime := state.AttachStateStore(cfg)
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	task := controller.SemanticTaskRef{TaskPath: binding.TaskPath, RequestID: binding.RequestID, ContractDigest: binding.TaskContractDigest}
	if binding.AttemptID != attempt.AttemptID || !task.Equal(attempt.SemanticTaskRef) {
		return fmt.Errorf("retained review belongs to a different runtime")
	}
	retained, err := runtime.LoadWorkerEndSnapshot()
	if err != nil {
		return err
	}
	actual, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if !state.EqualGitSnapshot(retained, actual) {
		return fmt.Errorf("retained review source differs from materialized workspace")
	}
	return nil
}

func executeControllerSuspend(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.EpisodeID == "" || command.EpisodeRevision == 0 {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution suspension requires episode authority")
	}
	admission, err := controller.CurrentSemanticAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	return store.SuspendExecution(admission, command.EpisodeID, command.EpisodeRevision)
}

func reenterMaterializedRuntimeCompletion(cfg config.AppConfig, result controller.ExecutionOperationResult) error {
	if result.Admission == nil {
		return nil
	}
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return fmt.Errorf("bind canonical runtime for controller reentry: %w", err)
	}
	if result.Admission.Attempt.PredecessorAttemptID == "" {
		return nil
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	authority, err := store.RuntimeMaterializationReentryAuthority(result.TransitionID)
	if err != nil {
		return err
	}
	runtime := state.AttachStateStore(runtimeCfg)
	if result.ParentSourceReview {
		if retained, err := reenterWithEquivalentReview(runtime, result.Admission.Workspace.Root, authority); err != nil {
			return err
		} else if retained {
			return verifyMaterializedRuntimeBinding(runtime, authority)
		}
	}
	if _, err := runtime.ReenterAcceptedCompletionFromController(authority); err != nil {
		return err
	}
	return verifyMaterializedRuntimeBinding(runtime, authority)
}

func verifyMaterializedRuntimeBinding(runtime *state.StateStore, authority state.ControllerReentryAuthority) error {
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if binding.TaskPath != authority.TaskPath {
		return fmt.Errorf("materialized runtime binding belongs to a different Task source")
	}
	switch binding.AttemptID {
	case authority.PredecessorAttemptID:
		if binding.TaskContractDigest != authority.PredecessorTaskContractDigest {
			return fmt.Errorf("materialized predecessor contract differs from canonical source")
		}
	case authority.AttemptID:
		if binding.TaskContractDigest != authority.TaskContractDigest {
			return fmt.Errorf("materialized successor contract differs from canonical source")
		}
	default:
		return fmt.Errorf("materialized runtime binding belongs to a different attempt")
	}
	return nil
}

func executeControllerRuntimeDisposition(store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.RuntimeDisposition == nil || command.RuntimeDisposition.ExpectedGeneration != command.ExpectedGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("runtime disposition requires exact generation")
	}
	return store.DisposeUnfinishedRuntime(*command.RuntimeDisposition)
}
