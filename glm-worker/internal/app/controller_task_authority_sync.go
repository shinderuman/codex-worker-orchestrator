package app

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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func executeControllerTaskAuthoritySync(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.SourceSnapshotID == "" || command.RevisedTask == nil {
		return controller.ExecutionOperationResult{}, fmt.Errorf("task authority sync requires exact source snapshot and revised Task identity")
	}
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	before, err := wf.CaptureTaskAuthoritySyncEvidence()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if err := verifyTaskAuthoritySyncRuntime(before, admission); err != nil {
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
		return controller.ExecutionOperationResult{}, err
	}
	return result, nil
}

func verifyTaskAuthoritySyncRuntime(before []byte, admission controller.Admission) error {
	var evidence workflow.TaskAuthoritySyncEvidence
	if err := json.Unmarshal(before, &evidence); err != nil {
		return fmt.Errorf("decode task authority sync runtime evidence: %w", err)
	}
	if evidence.Runtime == workflow.TaskAuthorityRuntimeUnstarted {
		return nil
	}
	if evidence.Runtime != workflow.TaskAuthorityRuntimeAttached || evidence.Binding == nil {
		return fmt.Errorf("task authority sync evidence is incomplete")
	}
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
