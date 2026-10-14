package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
	"io"
)

func executeControllerReentryReviewRepair(cfg config.AppConfig, store *controller.Store, sourceSnapshotID string) (controller.ExecutionOperationResult, error) {
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	before, err := wf.CaptureReentryReviewRepairEvidence()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	ref, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":reentry-review-repair:"+sourceSnapshotID, true, before)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	result, err := store.ImportParentSource(admission, sourceSnapshotID, ref)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := wf.CompleteReentryReviewRepair(before); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	return result, nil
}

func executeControllerExecutionRecovery(cfg config.AppConfig, store *controller.Store, transitionID string) (controller.ExecutionOperationResult, error) {
	result, err := store.RecoverExecutionOperation(transitionID)
	if err != nil {
		return result, err
	}
	before, err := store.ReentryReviewRepairEvidence(transitionID)
	if err != nil || before == nil {
		return result, err
	}
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		return result, err
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	return result, wf.CompleteReentryReviewRepair(before)
}
