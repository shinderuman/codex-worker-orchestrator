package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeControllerTaskAuthoritySync(cfg config.AppConfig, store *controller.Store, command ControllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.SourceSnapshotID == "" || command.RevisedTask == nil {
		return controller.ExecutionOperationResult{}, fmt.Errorf("task authority sync requires exact source snapshot and revised Task identity")
	}
	admission, runtimeCfg, err := taskAuthoritySyncAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	before, err := wf.CaptureTaskAuthoritySyncEvidence()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := VerifyTaskAuthoritySyncRuntime(before, admission); err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	ref, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":"+controller.TaskAuthoritySyncProducer+":"+command.SourceSnapshotID, true, before)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	result, err := store.SyncTaskAuthority(admission, command.SourceSnapshotID, ref, *command.RevisedTask)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := wf.CompleteTaskAuthoritySync(before); err != nil {
		return result, fmt.Errorf("task authority sync transition %s runtime completion failed: %w", result.TransitionID, err)
	}
	return result, nil
}

func taskAuthoritySyncAdmission(cfg config.AppConfig, store *controller.Store) (controller.Admission, config.AppConfig, error) {
	liveParentOnly, err := store.LiveParentOnlyExecution()
	if err != nil {
		return controller.Admission{}, cfg, err
	}
	if liveParentOnly {
		admission, err := store.ParentOnlyTaskAuthoritySyncAdmission(cfg)
		if err != nil {
			return controller.Admission{}, cfg, err
		}
		return admission, controller.WorkflowTaskConfig(cfg, store.Identity(), admission.Attempt.SemanticTaskRef), nil
	}
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return controller.Admission{}, cfg, err
	}
	admission, err := controller.CurrentSemanticAdmission(cfg, store)
	return admission, runtimeCfg, err
}

func VerifyTaskAuthoritySyncRuntime(before []byte, admission controller.Admission) error {
	var evidence TaskAuthoritySyncEvidence
	if err := json.Unmarshal(before, &evidence); err != nil {
		return fmt.Errorf("decode task authority sync runtime evidence: %w", err)
	}
	if admission.Attempt.ExecutionAuthority == controller.ExecutionAuthorityParentOnly {
		return verifyParentOnlyTaskSyncEvidence(evidence, admission)
	}
	if evidence.Runtime == TaskAuthorityRuntimeParentOnly {
		return fmt.Errorf("ordinary Task authority sync cannot accept parent-only smoke evidence")
	}
	if evidence.Runtime == TaskAuthorityRuntimeUnstarted {
		return verifyUnstartedTaskSyncEvidence(evidence)
	}
	if evidence.Runtime != TaskAuthorityRuntimeAttached || evidence.Binding == nil || evidence.TaskID == "" {
		return fmt.Errorf("task authority sync evidence is incomplete")
	}
	return verifyTaskAuthoritySyncAttached(evidence, admission)
}

func verifyParentOnlyTaskSyncEvidence(evidence TaskAuthoritySyncEvidence, admission controller.Admission) error {
	if evidence.Runtime != TaskAuthorityRuntimeParentOnly || evidence.TaskID == "" ||
		evidence.TaskID != admission.Attempt.AttemptID || evidence.Binding != nil {
		return fmt.Errorf("parent-only Task authority sync requires exact smoke attempt identity without Runtime binding")
	}
	return nil
}

func verifyUnstartedTaskSyncEvidence(evidence TaskAuthoritySyncEvidence) error {
	if evidence.TaskID != "" || evidence.Binding != nil {
		return fmt.Errorf("unstarted task authority sync must not claim retained runtime identity")
	}
	return nil
}

func verifyTaskAuthoritySyncAttached(evidence TaskAuthoritySyncEvidence, admission controller.Admission) error {
	if evidence.Binding.AttemptID != admission.Attempt.AttemptID ||
		evidence.Binding.TaskPath != admission.Attempt.SemanticTaskRef.TaskPath ||
		evidence.Binding.TaskContractDigest != admission.Attempt.SemanticTaskRef.ContractDigest {
		return fmt.Errorf("task authority sync runtime binding differs from the live attempt contract")
	}
	content, err := os.ReadFile(filepath.Join(admission.Workspace.Root, filepath.FromSlash(evidence.Binding.TaskPath)))
	if err != nil {
		return err
	}
	if digest := fmt.Sprintf("%x", sha256.Sum256(content)); digest != evidence.Binding.TaskContractDigest {
		return fmt.Errorf("task authority sync lane Task file differs from its observed authority")
	}
	return nil
}
