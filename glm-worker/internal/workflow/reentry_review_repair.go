package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"reflect"
	"strings"
)

type ReentryReviewRepairEvidence struct {
	TaskID     string                         `json:"task_id"`
	Binding    state.ControllerRuntimeBinding `json:"binding"`
	Checkpoint state.ResumeCheckpoint         `json:"checkpoint"`
	Open       *state.ParentReviewOpenState   `json:"open"`
}

func (w *Workflow) CaptureReentryReviewRepairEvidence() ([]byte, error) {
	if w.state.TaskStatus() == state.TaskStatusWaitingSolReview {
		return w.captureReviewedParentSourceRevision()
	}
	allowed, err := w.state.ReenteredReviewStopRepairable()
	if err != nil {
		return nil, err
	}
	if !allowed {
		if err := w.admitQueuedReentryReviewUpdate(); err != nil {
			return nil, err
		}
	}
	cp, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return nil, err
	}
	if err := w.verifyInterruptedRetention(cp); err != nil {
		return nil, err
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil, err
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return nil, err
	}
	open, err := w.state.CurrentParentReview()
	if err != nil {
		return nil, err
	}
	return json.Marshal(ReentryReviewRepairEvidence{TaskID: taskID, Binding: binding, Checkpoint: cp, Open: open})
}

func (w *Workflow) CompleteReentryReviewRepair(data []byte) error {
	source, err := w.validateReentryReviewRepairIdentity(data)
	if err != nil {
		return err
	}
	target, err := w.refreshReentryReviewPrompt(source.Checkpoint)
	if err != nil {
		return err
	}
	if err := w.attachStopRepositoryBoundary(&target); err != nil {
		return err
	}
	target.StopDirtyFiles, err = state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	current, err := w.state.LoadResumeCheckpoint()
	if err != nil && !reviewedSourceWithoutCheckpoint(w.state.TaskStatus(), source, err) {
		return err
	}
	open, err := w.state.CurrentParentReview()
	if err != nil {
		return err
	}
	if open == nil && w.state.TaskStatus() == state.TaskStatusInterrupted && reflect.DeepEqual(current, target) {
		return w.state.ClearFinalizationEvidence()
	}
	if err := w.replaceRetainedReentryReview(source, target); err != nil {
		return err
	}
	return w.state.ClearFinalizationEvidence()
}

func (w *Workflow) refreshReentryReviewPrompt(target state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	if target.WorkerResult == nil {
		return target, fmt.Errorf("reentry review checkpoint lacks its retained Worker report")
	}
	current, _, _, err := w.buildReviewCheckpoint(target.Request, *target.WorkerResult, target.ReviewNumber, target.AutoFixes)
	if err != nil {
		return target, err
	}
	target.Prompt = current.Prompt
	target.OriginalPrompt = current.OriginalPrompt
	return target, nil
}

func reviewedSourceWithoutCheckpoint(status state.TaskStatus, source ReentryReviewRepairEvidence, err error) bool {
	return status == state.TaskStatusWaitingSolReview && source.Open != nil && source.Open.Role == string(state.ReviewerRole) && errors.Is(err, state.ErrNoResumeCheckpoint)
}

func (w *Workflow) admitQueuedReentryReviewUpdate() error {
	open, err := w.state.CurrentParentReview()
	if err != nil {
		return err
	}
	retained, err := w.state.ReenteredReviewerStopRetained()
	if err != nil {
		return err
	}
	if open != nil || !retained {
		return fmt.Errorf("reentry review update is not admitted")
	}
	return nil
}

func (w *Workflow) replaceRetainedReentryReview(source ReentryReviewRepairEvidence, target state.ResumeCheckpoint) error {
	if source.Open != nil && source.Open.Role == string(state.ReviewerRole) {
		return w.state.ReviseParentSourceReviewCheckpoint(*source.Open, target)
	}
	if source.Open == nil {
		return w.state.UpdateReenteredReviewStop(source.Checkpoint, target)
	}
	return w.state.ReplaceReenteredReviewStop(source.Checkpoint, target)
}

func (w *Workflow) captureReviewedParentSourceRevision() ([]byte, error) {
	open, err := w.state.CurrentParentReview()
	if err != nil {
		return nil, err
	}
	if open == nil || open.Role != string(state.ReviewerRole) || open.PacketStatus != "NEEDS_SOL_REVIEW" {
		return nil, fmt.Errorf("parent source revision requires an actual independent Review")
	}
	result, err := w.previousImplementedReviewContext()
	if err != nil {
		return nil, err
	}
	request, err := w.parentSourceReviewRequest()
	if err != nil {
		return nil, err
	}
	cp, _, _, err := w.buildReviewCheckpoint(request, result, 1, 0)
	if err != nil {
		return nil, err
	}
	cp.SetStopKind(state.ResumeStopInterrupted)
	if err := w.attachStopRepositoryBoundary(&cp); err != nil {
		return nil, err
	}
	cp.StopDirtyFiles, err = state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return nil, err
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil, err
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return nil, err
	}
	return json.Marshal(ReentryReviewRepairEvidence{TaskID: taskID, Binding: binding, Checkpoint: cp, Open: open})
}

func (w *Workflow) parentSourceReviewRequest() (string, error) {
	request := w.state.ReadOr("last-request", "")
	if request == "" {
		return "", fmt.Errorf("parent source revision lacks its original request")
	}
	request += "\n親Codexの修正を含む現在の差分全体を独立Reviewする。旧Review結果や保存Worker報告を現在の差分の検証済み証拠として扱わない。"
	corrections, err := w.parentSourceReviewTargetCorrections()
	if err != nil {
		return "", err
	}
	if len(corrections) != 0 {
		request += "\n前回の報告は次のTARGETSが正規source proofを満たさず受理不能です。コードの検証結果を偽装せず、現物で解決可能なType.Memberまたは数値行範囲へ報告を訂正してください。\n" + strings.Join(corrections, "\n")
	}
	return request, nil
}

func (w *Workflow) parentSourceReviewTargetCorrections() ([]string, error) {
	binding, err := w.state.CurrentParentReviewBinding()
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, fmt.Errorf("source review revision lacks its exact Review binding")
	}
	var corrections []string
	for _, raw := range binding.Targets {
		target, err := reviewtarget.ParseTarget(raw)
		if err != nil {
			return nil, err
		}
		if err := reviewtarget.ValidateProofAddressable(w.config.RepoRoot, target); err != nil {
			corrections = append(corrections, err.Error())
		}
	}
	return corrections, nil
}

func (w *Workflow) validateReentryReviewRepairIdentity(data []byte) (ReentryReviewRepairEvidence, error) {
	var source ReentryReviewRepairEvidence
	if err := json.Unmarshal(data, &source); err != nil {
		return source, err
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return source, err
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return source, err
	}
	if source.TaskID != taskID {
		return source, fmt.Errorf("reentry review repair changed Task or Attempt binding")
	}
	present, err := controller.Exists(w.config)
	if err != nil {
		return source, err
	}
	if present {
		if err := w.bindReentryReviewRepairRuntime(source.Binding); err != nil {
			return source, err
		}
	} else if source.Binding != binding {
		return source, fmt.Errorf("reentry review repair changed Task or Attempt binding")
	}
	return source, nil
}

func (w *Workflow) bindReentryReviewRepairRuntime(source state.ControllerRuntimeBinding) error {
	store, err := controller.Open(w.config)
	if err != nil {
		return err
	}
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	task, err := store.ExecutionTaskForWorkspace(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if head.LiveAttemptID != source.AttemptID || task.TaskPath != source.TaskPath {
		return fmt.Errorf("reentry review repair changed runtime Task or attempt")
	}
	target := state.ControllerRuntimeBinding{AttemptID: source.AttemptID, TaskPath: task.TaskPath, TaskContractDigest: task.ContractDigest}
	current, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if current != source && current != target {
		return fmt.Errorf("reentry review repair runtime binding changed")
	}
	return w.state.SaveControllerRuntimeBinding(target)
}
