package workflow

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

func (w *Workflow) RecoverUnstartedMilestone() error {
	if err := w.admitCanonicalWorkflowMutation(); err != nil {
		return err
	}
	if err := w.verifyUnstartedMilestoneRuntime(); err != nil {
		return err
	}
	plan, err := w.unstartedMilestoneRecoveryPlan()
	if err != nil {
		return err
	}
	request, err := w.state.Read("last-request")
	if err != nil || request == "" {
		return fmt.Errorf("startup recovery request is unavailable: %w", err)
	}
	checkpoint, err := w.newExecutionMilestoneCheckpoint(request, plan.ActiveTaskPath, "worker-new", w.config.RoutineEffort, false, 1)
	if err != nil {
		return err
	}
	checkpoint, err = w.decorateExecutionMilestoneCheckpoint(checkpoint)
	if err != nil {
		return err
	}
	checkpoint.StopKind = state.ResumeStopInterrupted
	if err := w.attachStopRepositoryBoundary(&checkpoint); err != nil {
		return err
	}
	checkpoint.StopDirtyFiles, err = state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	return w.state.EnterStop(checkpoint)
}

func (w *Workflow) unstartedMilestoneRecoveryPlan() (*executionmilestone.Plan, error) {
	plan, err := executionmilestone.Load(w.state)
	if err != nil {
		return nil, err
	}
	if plan == nil || plan.CurrentIndex != 0 {
		return nil, fmt.Errorf("startup recovery requires an unstarted milestone plan")
	}
	if err := executionmilestone.ValidateAuthority(w.config, w.state, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (w *Workflow) verifyUnstartedMilestoneRuntime() error {
	if w.state.TaskStatus() != state.TaskStatusActive || w.canonicalAdmission.Lease.InFlightCallID != "" {
		return fmt.Errorf("startup recovery requires exact active runtime without an in-flight model call")
	}
	if _, err := w.state.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		return fmt.Errorf("startup recovery requires absent resume checkpoint: %w", err)
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	logs, err := taskview.ReadStatusTelemetry(w.state, taskID)
	if err != nil {
		return err
	}
	if len(logs) != 0 || w.state.ReadOr("worker.id", "") != "" || w.state.ReadOr("reviewer.id", "") != "" {
		return fmt.Errorf("startup recovery cannot replace existing model execution")
	}
	return nil
}
