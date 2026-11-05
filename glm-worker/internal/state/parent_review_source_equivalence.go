package state

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

// ParentReviewSourceEquivalence records a verified rebind of immutable review evidence.
// Parent source equivalence does not create a second review state authority.
type ParentReviewSourceEquivalence struct {
	OriginalSnapshot       SnapshotDigest `json:"original_snapshot"`
	OriginalReviewID       string         `json:"original_review_id"`
	ReviewerCallID         string         `json:"reviewer_call_id"`
	ReviewerResponseSHA256 string         `json:"reviewer_response_sha256"`
	WorkerCallID           string         `json:"worker_call_id"`
}

// ReenterEquivalentReviewFromController preserves an authenticated HIGH review
// after the caller proves source equivalence. Parent target evidence must be
// re-proven on the new source; this transition performs no model call.
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
	if !reflect.DeepEqual(completion, original) || completion.Source == nil || completion.Review == nil ||
		completion.Risk != string(packet.RiskHigh) || !completeParentReviewSnapshot(current) ||
		completion.Review.SourceEquivalence != nil {
		return false, fmt.Errorf("equivalent review reentry lacks its original accepted HIGH binding")
	}
	binding := completion.Review
	if binding.ID == "" || binding.PacketSHA256 == "" || len(binding.Packet) == 0 || binding.Proof == nil ||
		!sameParentReviewSnapshot(binding.Snapshot, *completion.Source) ||
		!sameParentReviewSnapshot(binding.Proof.Snapshot, binding.Snapshot) {
		return false, fmt.Errorf("equivalent review reentry lacks accepted independent review proof")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
	if reviewer.CallID == "" || reviewer.TaskID != taskID || reviewer.Role != ReviewerRole ||
		reviewer.CallType != CallTypeTask || reviewer.Outcome != "success" ||
		reviewer.PacketStatus != string(packet.StatusNeedsSolReview) ||
		!reviewer.ReadOnly || reviewer.SessionID == "" || reviewer.ModelAlias == "" ||
		reviewer.ResponseSHA256 == "" || worker.CallID == "" || worker.TaskID != taskID ||
		worker.Role != WorkerRole || worker.CallType != CallTypeTask ||
		worker.PacketStatus != string(packet.StatusImplemented) || worker.SessionID == "" {
		return false, fmt.Errorf("equivalent review reentry lacks verified model-call provenance")
	}
	// The values supplied by the workflow are not an authority by themselves.
	// Re-read the canonical model log and require these to be its final
	// Worker/independent Reviewer pair, not detached or fabricated call IDs.
	calls, err := s.ReadModelCallLogs(taskID)
	if err != nil {
		return false, err
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
		recordedReviewer.ModelAlias != reviewer.ModelAlias ||
		recordedReviewer.ReadOnly != reviewer.ReadOnly ||
		recordedReviewer.PacketStatus != reviewer.PacketStatus ||
		recordedReviewer.ResponseSHA256 != reviewer.ResponseSHA256 ||
		recordedReviewer.Outcome != reviewer.Outcome {
		return false, fmt.Errorf("equivalent Review model-call provenance no longer matches the canonical log")
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(state.Completion, completion) || state.Open != nil || state.Review != nil {
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
	state.Completion = nil
	state.Open = &ParentReviewOpenState{
		PacketStatus: string(packet.StatusNeedsSolReview),
		Role:         string(ReviewerRole),
		ModelAlias:   reviewer.ModelAlias,
		Risk:         string(packet.RiskHigh),
	}
	state.Review = &rebinding
	if err := validateParentReviewBindingState(state); err != nil {
		return false, err
	}
	reviewFile, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return false, err
	}
	statusFile, err := s.snapshotLifecycleFile("task.status")
	if err != nil {
		return false, err
	}
	if err := s.writeParentReviewState(state); err != nil {
		return false, s.rollbackLifecycleFiles(err, reviewFile, statusFile)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return false, s.rollbackLifecycleFiles(err, reviewFile, statusFile)
	}
	s.recordReopenedParentOutcome(taskID, ParentOriginUserAmendment, ParentCauseSolGate, string(packet.RiskHigh))
	return true, nil
}
