package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) bindCanonicalWorkflowAttempt(admission controller.Admission) error {
	target := state.ControllerRuntimeBinding{
		AttemptID:          admission.Attempt.AttemptID,
		TaskPath:           admission.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest,
	}
	previous, err := w.state.LoadControllerRuntimeBinding()
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read controller runtime binding: %w", err)
	case previous.AttemptID == target.AttemptID:
		if previous.TaskPath != target.TaskPath || previous.TaskContractDigest != target.TaskContractDigest {
			return fmt.Errorf("workflow runtime binding does not match canonical controller task")
		}
	default:
		if previous.AttemptID != admission.Attempt.PredecessorAttemptID {
			return fmt.Errorf("workflow session does not belong to the canonical attempt predecessor")
		}
		if err := w.reenterCanonicalWorkflow(admission); err != nil {
			return err
		}
	}
	if err := w.state.SaveControllerRuntimeBinding(target); err != nil {
		return err
	}
	return w.state.Remove(activeTaskStateKey)
}

func (w *Workflow) reenterCanonicalWorkflow(admission controller.Admission) error {
	if err := w.state.InvalidateAllSessions(); err != nil {
		return err
	}
	if err := state.CaptureGitBaseline(w.config, w.state); err != nil {
		return err
	}
	if err := w.captureQualitySurfaceBaseline(); err != nil {
		return err
	}
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	if !checkpoint.IsStopped() {
		return nil
	}
	return w.reenterCanonicalCheckpoint(admission, checkpoint)
}

func (w *Workflow) reenterCanonicalCheckpoint(admission controller.Admission, previous state.ResumeCheckpoint) error {
	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: w.config.WorkerModel, Effort: w.config.EscalatedEffort,
		Request: previous.Request, Decision: previous.Decision,
		ExecutionMilestoneID: previous.ExecutionMilestoneID,
		StopKind:             state.ResumeStopInterrupted,
	}
	checkpoint.OriginalPrompt = w.newWorkerTaskPrompt(checkpoint.Request, admission.Attempt.SemanticTaskRef.TaskPath)
	if checkpoint.Decision != "" {
		checkpoint.OriginalPrompt += "\n承認済みの意味判断:\n" + checkpoint.Decision
	}
	checkpoint.Prompt = checkpoint.OriginalPrompt
	if err := w.attachStopRepositoryBoundary(&checkpoint); err != nil {
		return err
	}
	var err error
	checkpoint.StopDirtyFiles, err = state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	return w.state.EnterStop(checkpoint)
}
