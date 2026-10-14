package workflow

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) PrepareReenteredReview() error {
	if err := w.admitReenteredReviewPreparation(); err != nil {
		return err
	}
	result, err := w.previousImplementedReviewContext()
	if err != nil {
		return err
	}
	request := w.state.ReadOr("last-request", "")
	if request == "" {
		return fmt.Errorf("review preparation has no original request")
	}
	if err := w.admitCanonicalReenteredReview(); err != nil {
		return err
	}
	request += "\n親のソース改訂を含む現在の差分全体を独立Reviewする。保存されたWorker報告は改訂前の背景資料であり、現在の差分や検証済み状態の代用ではない。現在のACTIVE Task本文とIntegration baseからの全差分を正とする。"
	checkpoint, _, _, err := w.buildReviewCheckpoint(request, result, 1, 0)
	if err != nil {
		return err
	}
	snapshot, stopped, err := w.captureWorkerEndSnapshot()
	if err != nil {
		return err
	}
	if stopped {
		return fmt.Errorf("reentered review snapshot was rejected")
	}
	if err := w.state.SaveReviewStartSnapshot(snapshot); err != nil {
		return err
	}
	checkpoint.SetStopKind(state.ResumeStopInterrupted)
	if err := w.attachStopRepositoryBoundary(&checkpoint); err != nil {
		return err
	}
	checkpoint.StopDirtyFiles, err = state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	return w.state.EnterReenteredReviewStop(checkpoint)
}

func (w *Workflow) admitCanonicalReenteredReview() error {
	if err := w.admitCanonicalWorkflowMutation(); err != nil {
		return err
	}
	if w.canonicalAdmission == nil || w.canonicalAdmission.Attempt.PredecessorAttemptID == "" || w.canonicalAdmission.Lease.InFlightCallID != "" {
		return fmt.Errorf("review preparation requires the canonical idle reentered attempt")
	}
	return nil
}

func (w *Workflow) admitReenteredReviewPreparation() error {
	_, checkpointErr := w.state.LoadResumeCheckpoint()
	if w.state.TaskStatus() != state.TaskStatusWaitingSolReview || !errors.Is(checkpointErr, state.ErrNoResumeCheckpoint) {
		return fmt.Errorf("review preparation requires a reentered accepted completion without a checkpoint")
	}
	open, err := w.state.CurrentParentReview()
	if err != nil {
		return err
	}
	if open == nil || open.Role != "" || open.ModelAlias != "" || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return fmt.Errorf("review preparation cannot replace an actual model review")
	}
	return nil
}

func (w *Workflow) previousImplementedReviewContext() (packet.Result, error) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return packet.Result{}, err
	}
	logs, err := w.state.ReadModelCallLogs(taskID)
	if err != nil {
		return packet.Result{}, err
	}
	for i := len(logs) - 1; i >= 0; i-- {
		call := logs[i]
		if call.CallType != state.CallTypeTask || call.Role != state.WorkerRole || call.Outcome != acceptedResultCallOutcome || call.PacketStatus != string(packet.StatusImplemented) {
			continue
		}
		if !savedWorkerReviewContextMatches(call, taskID) {
			return packet.Result{}, fmt.Errorf("saved worker review context identity differs")
		}
		result, err := packet.ParseStructured([]byte(call.Response))
		if err != nil {
			return result, err
		}
		if err := packet.ValidateWorkerResult(result); err != nil {
			return result, err
		}
		return result, nil
	}
	return packet.Result{}, fmt.Errorf("reentered review has no saved successful Worker packet")
}

func savedWorkerReviewContextMatches(call state.ModelCallLog, taskID string) bool {
	return call.TaskID == taskID && call.SessionID != "" && call.ResponseSHA256 == resultRecoveryDigest([]byte(call.Response))
}
