package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type controllerExecutionCommand struct {
	RuntimeDisposition     *controller.RuntimeDispositionInput       `json:"runtime_disposition,omitempty"`
	SourceSnapshotID       string                                    `json:"source_snapshot_id,omitempty"`
	CandidateID            string                                    `json:"candidate_id,omitempty"`
	Action                 string                                    `json:"action"`
	ExpectedGeneration     uint64                                    `json:"expected_generation"`
	EpisodeID              string                                    `json:"episode_id,omitempty"`
	EpisodeRevision        uint64                                    `json:"episode_revision,omitempty"`
	SuspensionID           string                                    `json:"suspension_id,omitempty"`
	WorkspaceID            string                                    `json:"workspace_id,omitempty"`
	SealRef                *controller.EvidenceObjectRef             `json:"seal_ref,omitempty"`
	TransitionID           string                                    `json:"transition_id,omitempty"`
	UserTermination        *controller.UserTaskTransitionInput       `json:"user_termination,omitempty"`
	ExecutionTermination   *controller.UserExecutionTransitionInput  `json:"execution_termination,omitempty"`
	RevisedTask            *controller.SemanticTaskRef               `json:"revised_task,omitempty"`
	AcceptedResultRecovery *workflow.AcceptedResultRuleRecoveryInput `json:"accepted_result_recovery,omitempty"`
}

const (
	controllerExecutionSurface   = "controller-execution"
	controllerPublicationSurface = "controller-publication"
)

func runControllerOperations(args []string, loadConfig func() (config.AppConfig, error), stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag {
		return false, nil
	}
	var command any
	switch args[1] {
	case controllerExecutionSurface:
		command = &controllerExecutionCommand{}
	case controllerPublicationSurface:
		command = &controllerPublicationCommand{}
	default:
		return false, nil
	}
	cfg, store, err := decodeControllerCommand(loadConfig, stdin, command)
	if err != nil {
		return true, err
	}
	lock, err := acquireWorkflowLock(cfg)
	if err != nil {
		return true, err
	}
	defer func() { _ = lock.Close() }()
	result, err := dispatchControllerOperation(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, result)
}

func dispatchControllerOperation(cfg config.AppConfig, store *controller.Store, command any) (any, error) {
	switch value := command.(type) {
	case *controllerExecutionCommand:
		return executeControllerExecution(cfg, store, *value)
	case *controllerPublicationCommand:
		return executeControllerPublication(cfg, store, *value)
	default:
		return nil, fmt.Errorf("unsupported controller command type")
	}
}

func executeControllerExecution(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
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

func executeControllerExecutionAction(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
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

func executeControllerParentSourceAction(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
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

func executeControllerUserExecutionTermination(store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
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

func executeControllerUserTermination(store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
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
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if err := wf.PrepareReenteredReview(); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head}, err
}

func executeControllerCleanup(store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.SealRef == nil || command.WorkspaceID == "" {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution cleanup requires sealed workspace identity")
	}
	return store.CleanupExecution(controller.CleanupExecutionInput{ExpectedGeneration: command.ExpectedGeneration, WorkspaceID: command.WorkspaceID, SealRef: *command.SealRef})
}

func executeControllerAcceptedResultRecovery(cfg config.AppConfig, store *controller.Store, input *workflow.AcceptedResultRuleRecoveryInput) (controller.ExecutionOperationResult, error) {
	if input == nil {
		return controller.ExecutionOperationResult{}, fmt.Errorf("accepted-result recovery requires exact source identity")
	}
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
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
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if err := wf.RecoverUnstartedMilestone(); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	head, err := store.LoadHead()
	return controller.ExecutionOperationResult{Head: head}, err
}

func executeControllerMaterialize(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	result, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: command.ExpectedGeneration, EpisodeID: command.EpisodeID, EpisodeRevision: command.EpisodeRevision, SuspensionID: command.SuspensionID})
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if result.Admission != nil && result.Admission.Attempt.ExecutionAuthority == controller.ExecutionAuthorityParentOnly {
		return result, nil
	}
	if err := reenterMaterializedRuntimeCompletion(cfg, result); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if result.ParentSourceReview {
		cfg.RepoRoot = result.Admission.Workspace.Root
		if _, err := executeControllerReentryReview(cfg, store); err != nil {
			return controller.ExecutionOperationResult{}, err
		}
	}
	return result, nil
}

func executeControllerSuspend(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.EpisodeID == "" || command.EpisodeRevision == 0 {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution suspension requires episode authority")
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
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
	_, err = state.AttachStateStore(runtimeCfg).ReenterAcceptedCompletionFromController(authority)
	return err
}

func decodeControllerCommand(loadConfig func() (config.AppConfig, error), stdin io.Reader, command any) (config.AppConfig, *controller.Store, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cfg, nil, err
	}
	store, err := openControllerSemanticStore(cfg)
	if err != nil {
		return cfg, nil, err
	}
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(command); err != nil {
		return cfg, nil, err
	}
	if err := requireControllerEvidenceEOF(decoder); err != nil {
		return cfg, nil, err
	}
	return cfg, store, nil
}

func executeControllerRuntimeDisposition(store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.RuntimeDisposition == nil || command.RuntimeDisposition.ExpectedGeneration != command.ExpectedGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("runtime disposition requires exact generation")
	}
	return store.DisposeUnfinishedRuntime(*command.RuntimeDisposition)
}
