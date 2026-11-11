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
	if !validEquivalentAcceptedOutcome(completion, original, current) {
		return nil, fmt.Errorf("equivalent review reentry lacks its original accepted HIGH binding")
	}
	binding := completion.Review
	if !validEquivalentReviewProof(binding, *completion.Source) {
		return nil, fmt.Errorf("equivalent review reentry lacks accepted independent review proof")
	}
	return binding, nil
}

func validEquivalentAcceptedOutcome(completion, original *ParentCompletionOutcome, current SnapshotDigest) bool {
	if completion == nil || original == nil {
		return false
	}
	return reflect.DeepEqual(completion, original) && completion.Source != nil &&
		completion.Review != nil && completion.Risk == string(packet.RiskHigh) &&
		completeParentReviewSnapshot(current) && completion.Review.SourceEquivalence == nil
}

func validEquivalentReviewProof(binding *ParentReviewBinding, source SnapshotDigest) bool {
	if binding.Proof == nil {
		return false
	}
	return binding.ID != "" && binding.PacketSHA256 != "" && binding.Packet != "" &&
		sameParentReviewSnapshot(binding.Snapshot, source) &&
		sameParentReviewSnapshot(binding.Proof.Snapshot, binding.Snapshot)
}

func validateEquivalentCallIdentity(taskID string, reviewer, worker ModelCallLog) error {
	if !validEquivalentReviewerIdentity(taskID, reviewer) || !validEquivalentWorkerIdentity(taskID, worker) {
		return fmt.Errorf("equivalent review reentry lacks verified model-call provenance")
	}
	return nil
}

func validEquivalentReviewerIdentity(taskID string, reviewer ModelCallLog) bool {
	return reviewer.CallID != "" && reviewer.TaskID == taskID && reviewer.Role == ReviewerRole &&
		reviewer.CallType == CallTypeTask && reviewer.Outcome == "success" &&
		reviewer.PacketStatus == string(packet.StatusNeedsSolReview) &&
		reviewer.ReadOnly && reviewer.SessionID != "" && reviewer.ModelAlias != "" &&
		reviewer.ResponseSHA256 != ""
}

func validEquivalentWorkerIdentity(taskID string, worker ModelCallLog) bool {
	return worker.CallID != "" && worker.TaskID == taskID && worker.Role == WorkerRole &&
		worker.CallType == CallTypeTask && worker.PacketStatus == string(packet.StatusImplemented) &&
		worker.SessionID != ""
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
	if !sameEquivalentWorkerEvidence(recordedWorker, worker) || !sameEquivalentReviewerEvidence(recordedReviewer, reviewer) {
		return fmt.Errorf("equivalent Review model-call provenance no longer matches the canonical log")
	}
	return nil
}

func sameEquivalentWorkerEvidence(a, b ModelCallLog) bool {
	return a.CallID == b.CallID && a.SessionID == b.SessionID &&
		a.PacketStatus == b.PacketStatus && a.Outcome == b.Outcome &&
		a.ResponseSHA256 == b.ResponseSHA256
}

func sameEquivalentReviewerEvidence(a, b ModelCallLog) bool {
	return a.CallID == b.CallID && a.SessionID == b.SessionID &&
		a.ModelAlias == b.ModelAlias && a.ReadOnly == b.ReadOnly &&
		a.PacketStatus == b.PacketStatus && a.ResponseSHA256 == b.ResponseSHA256 &&
		a.Outcome == b.Outcome
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
