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

func applyReentrySourceHandoff(cfg config.AppConfig, output *parentHandoffOutput, head controller.RepositoryControllerHead, revised controller.SemanticTaskRef, store *controller.Store) bool {
	if head.LiveLeaseID == "" || head.PendingTransitionID != "" || head.ExecutionTaskRef == nil || revised.Empty() || revised.TaskPath != head.ExecutionTaskRef.TaskPath {
		return false
	}
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return false
	}
	admission, primary, err := store.PreviewParentSourceImport(runtimeCfg.RepoRoot)
	if err != nil || admission.Head.ControllerGeneration != head.ControllerGeneration {
		return false
	}
	command, err := reentrySourceCommand(runtimeCfg, admission, primary, revised)
	if err != nil {
		return false
	}
	data, err := json.Marshal(command)
	if err != nil {
		return false
	}
	action := controllerExecutionSurface
	output.RequiredAction = &action
	output.RequiredActionParameters = map[string]string{"controller_command_json": string(data)}
	output.AllowedActions = []string{action, "controller-evidence"}
	return true
}

func reentrySourceCommand(cfg config.AppConfig, admission controller.Admission, primary controller.WorkspaceSnapshot, revised controller.SemanticTaskRef) (controllerExecutionCommand, error) {
	command := controllerExecutionCommand{ExpectedGeneration: admission.Head.ControllerGeneration}
	runtime := state.AttachStateStore(cfg)
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return command, err
	}
	if err := validateReentrySourceBinding(binding, admission); err != nil {
		return command, err
	}
	if runtime.TaskStatus() == state.TaskStatusWaitingSolReview {
		wf := workflow.NewWorkflow(cfg, runtime, nil, io.Discard)
		if err := wf.ValidateReenteredReviewPreparation(); err != nil {
			return command, err
		}
		command.Action = "prepare-reentry-review"
		return command, nil
	}
	retained, err := runtime.ReenteredReviewerStopRetained()
	if err != nil {
		return command, err
	}
	if !retained || binding.AttemptID != admission.Attempt.AttemptID {
		return command, fmt.Errorf("parent source import lacks its bound retained reentry review")
	}
	command.Action = "repair-reentry-review"
	command.SourceSnapshotID = primary.ID
	command.RevisedTask = &revised
	return command, nil
}

func validateReentrySourceBinding(binding state.ControllerRuntimeBinding, admission controller.Admission) error {
	attempt := admission.Attempt
	if attempt.PredecessorAttemptID == "" || admission.Lease.InFlightCallID != "" {
		return fmt.Errorf("parent source import requires an idle reentered attempt")
	}
	if binding.TaskPath != attempt.SemanticTaskRef.TaskPath || binding.TaskContractDigest != attempt.SemanticTaskRef.ContractDigest {
		return fmt.Errorf("reentry source Runtime Task binding differs from controller")
	}
	if binding.AttemptID != attempt.AttemptID && binding.AttemptID != attempt.PredecessorAttemptID {
		return fmt.Errorf("reentry source Runtime Attempt binding differs from controller")
	}
	return nil
}
