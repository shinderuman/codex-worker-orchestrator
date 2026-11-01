package state

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func breakTaskIDRead(t *testing.T, st *StateStore, taskID string) {
	t.Helper()
	if err := os.Remove(st.Path("task.id")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.Path("task.id"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(st.Path("task.id"))
		_ = st.Write("task.id", taskID)
	})
}

func TestParentReviewResolutionPropagatesTaskIDReadErrors(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	recordPacket(t, st, packet.StatusNeedsSolReview, packet.RiskHigh, ParentReviewProducer{Role: "reviewer", Model: "sonnet"})

	breakTaskIDRead(t, st, taskID)
	if _, err := st.CurrentParentReview(); err == nil {
		t.Fatal("unreadable task.id hid its read error from the open review lookup")
	}
	if _, resolved, err := st.resolveParentReviewState(ParentOutcomeAccepted, ParentOriginCodexReview, ""); err == nil || resolved {
		t.Fatalf("unreadable task.id resolved an open review: resolved=%v err=%v", resolved, err)
	}
	if resolved, err := st.RecordParentOutcome(ParentOutcomeAccepted, ParentOriginCodexReview, ""); err == nil || resolved {
		t.Fatalf("unreadable task.id resolved an open review through the public owner: resolved=%v err=%v", resolved, err)
	}

	if err := os.Remove(st.Path("task.id")); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("failed resolution cleared the open review: %q", label)
	}
	if resolved, err := st.RecordParentOutcome(ParentOutcomeAccepted, ParentOriginCodexReview, ""); err != nil || !resolved {
		t.Fatalf("restored task.id could not resolve the review: resolved=%v err=%v", resolved, err)
	}
	if label := st.OpenParentReviewLabel(); label != roundCommentNone {
		t.Fatalf("resolved review stayed open: %q", label)
	}
}

func TestParentReviewResolutionTreatsMissingTaskIDAsQuiescent(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(t, st, packet.StatusNeedsSolReview, packet.RiskLow, ParentReviewProducer{Role: "reviewer", Model: "haiku"})
	if err := st.Remove("task.id"); err != nil {
		t.Fatal(err)
	}
	if current, err := st.CurrentParentReview(); err != nil || current != nil {
		t.Fatalf("missing task.id surfaced as an open review error: %#v %v", current, err)
	}
	if _, resolved, err := st.resolveParentReviewState(ParentOutcomeAccepted, ParentOriginCodexReview, ""); err != nil || resolved {
		t.Fatalf("missing task.id resolved an open review: resolved=%v err=%v", resolved, err)
	}
}
