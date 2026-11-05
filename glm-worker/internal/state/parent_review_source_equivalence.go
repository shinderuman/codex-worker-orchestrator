package state

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentReviewSourceEquivalence struct {
	OriginalSnapshot       SnapshotDigest `json:"original_snapshot"`
	OriginalReviewID       string         `json:"original_review_id"`
	ReviewerCallID         string         `json:"reviewer_call_id"`
	ReviewerResponseSHA256 string         `json:"reviewer_response_sha256"`
	WorkerCallID           string         `json:"worker_call_id"`
}

func (s *StateStore) ReenterEquivalentReviewFromController(
	authority ControllerReentryAuthority,
	original *ParentCompletionOutcome,
	reviewer, worker ModelCallLog,
	current SnapshotDigest,
) (bool, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return false, nil
	}
	completion, err := s.controllerReentryCompletionAuthority(authority)
	if err != nil {
		return false, err
	}
	binding, err := validateAcceptedEquivalentReview(completion, original, current)
	if err != nil {
		return false, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
	if err := validateEquivalentCallIdentity(taskID, reviewer, worker); err != nil {
		return false, err
	}
	if err := s.verifyEquivalentModelCallLog(taskID, reviewer, worker); err != nil {
		return false, err
	}
	return s.applyEquivalentReviewReentry(taskID, completion, binding, reviewer, worker, current)
}

func validateAcceptedEquivalentReview(completion, original *ParentCompletionOutcome, current SnapshotDigest) (*ParentReviewBinding, error) {
	if !reflect.DeepEqual(completion, original) || completion.Source == nil || completion.Review == nil ||
		completion.Risk != string(packet.RiskHigh) || !completeParentReviewSnapshot(current) ||
		completion.Review.SourceEquivalence != nil {
		return nil, fmt.Errorf("equivalent review reentry lacks its original accepted HIGH binding")
	}
	binding := completion.Review
	if binding.ID == "" || binding.PacketSHA256 == "" || binding.Packet == "" || binding.Proof == nil ||
		!sameParentReviewSnapshot(binding.Snapshot, *completion.Source) ||
		!sameParentReviewSnapshot(binding.Proof.Snapshot, binding.Snapshot) {
		return nil, fmt.Errorf("equivalent review reentry lacks accepted independent review proof")
	}
	return binding, nil
}

func validateEquivalentCallIdentity(taskID string, reviewer, worker ModelCallLog) error {
	if reviewer.CallID == "" || reviewer.TaskID != taskID || reviewer.Role != ReviewerRole ||
		reviewer.CallType != CallTypeTask || reviewer.Outcome != "success" ||
		reviewer.PacketStatus != string(packet.StatusNeedsSolReview) ||
		!reviewer.ReadOnly || reviewer.SessionID == "" || reviewer.ModelAlias == "" ||
		reviewer.ResponseSHA256 == "" || worker.CallID == "" || worker.TaskID != taskID ||
		worker.Role != WorkerRole || worker.CallType != CallTypeTask ||
		worker.PacketStatus != string(packet.StatusImplemented) || worker.SessionID == "" {
		return fmt.Errorf("equivalent review reentry lacks verified model-call provenance")
	}
	return nil
}

func (s *StateStore) verifyEquivalentModelCallLog(taskID string, reviewer, worker ModelCallLog) error {
	calls, err := s.ReadModelCallLogs(taskID)
	if err != nil {
		return err
	}
	var recordedWorker, recordedReviewer ModelCallLog
	for _, call := range calls {
		if call.CallType != CallTypeTask {
			continue
		}
		switch call.Role {
		case WorkerRole:
			recordedWorker = call
			recordedReviewer = ModelCallLog{}
		case ReviewerRole:
			recordedReviewer = call
		}
	}
	if recordedWorker.CallID != worker.CallID || recordedWorker.SessionID != worker.SessionID ||
		recordedWorker.PacketStatus != worker.PacketStatus || recordedWorker.Outcome != worker.Outcome ||
		recordedWorker.ResponseSHA256 != worker.ResponseSHA256 ||
		recordedReviewer.CallID != reviewer.CallID || recordedReviewer.SessionID != reviewer.SessionID ||
		recordedReviewer.ModelAlias != reviewer.ModelAlias || recordedReviewer.ReadOnly != reviewer.ReadOnly ||
		recordedReviewer.PacketStatus != reviewer.PacketStatus ||
		recordedReviewer.ResponseSHA256 != reviewer.ResponseSHA256 ||
		recordedReviewer.Outcome != reviewer.Outcome {
		return fmt.Errorf("equivalent Review model-call provenance no longer matches the canonical log")
	}
	return nil
}

func (s *StateStore) applyEquivalentReviewReentry(
	taskID string,
	completion *ParentCompletionOutcome,
	binding *ParentReviewBinding,
	reviewer, worker ModelCallLog,
	current SnapshotDigest,
) (bool, error) {
	reviewState, err := s.loadParentReviewState()
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(reviewState.Completion, completion) || reviewState.Open != nil || reviewState.Review != nil {
		return false, fmt.Errorf("equivalent review reentry authority changed")
	}
	rebinding := *binding
	rebinding.Snapshot = current
	rebinding.Coverage = nil
	rebinding.Proof = nil
	rebinding.SourceEquivalence = &ParentReviewSourceEquivalence{
		OriginalSnapshot:       binding.Snapshot,
		OriginalReviewID:       binding.ID,
		ReviewerCallID:         reviewer.CallID,
		ReviewerResponseSHA256: reviewer.ResponseSHA256,
		WorkerCallID:           worker.CallID,
	}
	reviewState.Completion = nil
	reviewState.Open = &ParentReviewOpenState{
		PacketStatus: string(packet.StatusNeedsSolReview),
		Role:         string(ReviewerRole),
		ModelAlias:   reviewer.ModelAlias,
		Risk:         string(packet.RiskHigh),
	}
	reviewState.Review = &rebinding
	if err := validateParentReviewBindingState(reviewState); err != nil {
		return false, err
	}
	if err := s.writeEquivalentReviewState(reviewState); err != nil {
		return false, err
	}
	s.recordReopenedParentOutcome(taskID, ParentOriginUserAmendment, ParentCauseSolGate, string(packet.RiskHigh))
	return true, nil
}

func (s *StateStore) writeEquivalentReviewState(reviewState ParentReviewState) error {
	reviewFile, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	statusFile, err := s.snapshotLifecycleFile("task.status")
	if err != nil {
		return err
	}
	if err := s.writeParentReviewState(reviewState); err != nil {
		return s.rollbackLifecycleFiles(err, reviewFile, statusFile)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, reviewFile, statusFile)
	}
	return nil
}
