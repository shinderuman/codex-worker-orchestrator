package workflow

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeControllerReentryReviewRepair(cfg config.AppConfig, store *controller.Store, sourceSnapshotID string, task *controller.SemanticTaskRef) (controller.ExecutionOperationResult, error) {
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	before, err := wf.CaptureReentryReviewRepairEvidence()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	admission, err := controller.CurrentSemanticAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	ref, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":reentry-review-repair:"+sourceSnapshotID, true, before)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	target := admission.Attempt.SemanticTaskRef
	if task != nil {
		target = *task
	}
	result, err := store.ImportParentSource(admission, sourceSnapshotID, ref, target)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := wf.CompleteReentryReviewRepair(before); err != nil {
		return result, fmt.Errorf("reentry review transition %s runtime completion failed: %w", result.TransitionID, err)
	}
	return result, nil
}

func executeControllerExecutionRecovery(cfg config.AppConfig, store *controller.Store, transitionID string) (controller.ExecutionOperationResult, error) {
	result, err := store.RecoverExecutionOperation(transitionID)
	if err != nil {
		return result, err
	}
	record, _, err := store.LoadTransition(transitionID)
	if err != nil {
		return result, err
	}
	if record.Kind == "MATERIALIZE_EXECUTION" {
		return result, completeMaterializedRuntime(cfg, store, result)
	}
	before, err := store.ReentryReviewRepairEvidence(transitionID)
	if err != nil {
		return result, err
	}
	var syncBefore []byte
	if before == nil {
		syncBefore, err = store.TaskAuthoritySyncEvidence(transitionID)
		if err != nil {
			return result, err
		}
	}
	if before == nil && syncBefore == nil {
		return result, nil
	}
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return result, fmt.Errorf("controller execution transition %s runtime config failed: %w", transitionID, err)
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	if before != nil {
		err = wf.CompleteReentryReviewRepair(before)
	} else {
		err = wf.CompleteTaskAuthoritySync(syncBefore)
	}
	if err != nil {
		return result, fmt.Errorf("controller execution transition %s runtime completion failed: %w", transitionID, err)
	}
	return result, nil
}
