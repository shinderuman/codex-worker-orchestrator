package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestRecoverParentActionBeginRestoresDecisionReviewState(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.openParentReviewState(
		string(packet.StatusNeedsSolDecision),
		string(packet.RiskHigh),
		ParentReviewProducer{Role: string(ReviewerRole), Model: "reviewer-model"},
	); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}
	if current, err := st.CurrentParentReview(); err != nil || current != nil {
		t.Fatalf("begin must consume the current review: review=%#v err=%v", current, err)
	}

	if _, err := st.RecoverParentActionBeginFromState(); err != nil {
		t.Fatal(err)
	}
	current, err := st.CurrentParentReview()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil ||
		current.PacketStatus != string(packet.StatusNeedsSolDecision) ||
		current.Risk != string(packet.RiskHigh) ||
		current.Role != string(ReviewerRole) ||
		current.ModelAlias != "reviewer-model" {
		t.Fatalf("recovered review = %#v", current)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("recovered lifecycle: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverParentActionBeginRestoresFixReviewBinding(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	reviewID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	reviewState := ParentReviewState{
		Version: parentReviewStateVersion,
		TaskID:  taskID,
		Open: &ParentReviewOpenState{
			PacketStatus: string(packet.StatusNeedsSolReview),
			Role:         string(ReviewerRole),
			ModelAlias:   "reviewer-model",
			Risk:         string(packet.RiskHigh),
		},
		Review: &ParentReviewBinding{
			ID:           reviewID,
			PacketSHA256: strings.Repeat("a", 64),
			Targets:      []string{"internal/example.go"},
			SolQuestion:  "verify the current target",
			Snapshot: SnapshotDigest{
				Head:           "head-digest",
				IndexDigest:    "index-digest",
				WorktreeDigest: "worktree-digest",
			},
		},
	}
	if err := st.writeParentReviewState(reviewState); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}

	if _, err := st.BeginParentFix(ParentOriginCodexReview, ParentCauseParentOrchestration); err != nil {
		t.Fatal(err)
	}
	if current, err := st.CurrentParentReview(); err != nil || current != nil {
		t.Fatalf("begin must consume the current review: review=%#v err=%v", current, err)
	}
	if binding, err := st.CurrentParentReviewBinding(); err != nil || binding != nil {
		t.Fatalf("begin must consume the current binding: binding=%#v err=%v", binding, err)
	}

	if _, err := st.RecoverParentActionBeginFromState(); err != nil {
		t.Fatal(err)
	}
	current, err := st.CurrentParentReview()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil ||
		current.PacketStatus != string(packet.StatusNeedsSolReview) ||
		current.Risk != string(packet.RiskHigh) ||
		current.Role != string(ReviewerRole) ||
		current.ModelAlias != "reviewer-model" {
		t.Fatalf("recovered review = %#v", current)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil ||
		binding.ID != reviewID ||
		binding.PacketSHA256 != strings.Repeat("a", 64) ||
		len(binding.Targets) != 1 || binding.Targets[0] != "internal/example.go" ||
		binding.SolQuestion != "verify the current target" ||
		binding.Snapshot != reviewState.Review.Snapshot {
		t.Fatalf("recovered binding = %#v", binding)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("recovered lifecycle: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverParentActionBeginAllowsAlreadyRestoredReviewOnRetry(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.openParentReviewState(
		string(packet.StatusNeedsSolDecision),
		string(packet.RiskLow),
		ParentReviewProducer{Role: string(ReviewerRole), Model: "reviewer-model"},
	); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}
	record, err := st.loadParentActionBegin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.restoreParentActionBeginSnapshots(record); err != nil {
		t.Fatal(err)
	}

	if _, err := st.RecoverParentActionBeginFromState(); err != nil {
		t.Fatal(err)
	}
	current, err := st.CurrentParentReview()
	if err != nil || current == nil || current.PacketStatus != string(packet.StatusNeedsSolDecision) {
		t.Fatalf("retry recovery review = %#v err=%v", current, err)
	}
}

func TestValidateParentActionBeginReviewSnapshotRejectsCompletionWithOpenReview(t *testing.T) {
	review := ParentReviewState{
		Version: parentReviewStateVersion,
		TaskID:  "task-1",
		Open: &ParentReviewOpenState{
			PacketStatus: string(packet.StatusNeedsSolReview),
			Role:         string(ReviewerRole),
			ModelAlias:   "reviewer-model",
			Risk:         string(packet.RiskLow),
		},
		Completion: &ParentCompletionOutcome{
			Terminal: SessionRotationTerminalAccept,
			Risk:     string(packet.RiskLow),
		},
	}
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	record := parentActionBeginRecord{
		TaskID: review.TaskID,
		Review: parentActionBeginSnapshot{Exists: true, Data: data},
	}
	if err := validateParentActionBeginReviewSnapshot(record); err == nil || !strings.Contains(err.Error(), "completion outcome exists with an open parent review") {
		t.Fatalf("invalid completion/open snapshot was accepted: %v", err)
	}
}
