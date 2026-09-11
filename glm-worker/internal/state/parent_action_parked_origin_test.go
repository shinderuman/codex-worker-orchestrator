package state

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func newParkedPlanStore(t *testing.T) *StateStore {
	t.Helper()
	st, err := NewStateStore(config.AppConfig{
		StateBase: filepath.Join(t.TempDir(), "state"),
		RepoHash:  "parkedplan",
		RepoRoot:  filepath.Join(t.TempDir(), "repo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}

func parkFromStatus(t *testing.T, st *StateStore, from TaskStatus) {
	t.Helper()
	if err := st.SetTaskStatus(from); err != nil {
		t.Fatal(err)
	}
	record := ParkRecord{ParkID: "0123456789abcdef", FromStatus: from, TaskID: st.ReadOr("task.id", ""), RepoRoot: "repo", Head: "head", Worktree: "worktree", Branch: "branch"}
	if err := st.EnterParked(record); err != nil {
		t.Fatal(err)
	}
}

func setParkedReviewLabel(t *testing.T, st *StateStore, label string) {
	t.Helper()
	current, err := st.loadParentReviewState()
	if err != nil {
		t.Fatal(err)
	}
	if label == roundCommentNone {
		current.Open = nil
	} else {
		current.Open = &ParentReviewOpenState{PacketStatus: label}
	}
	if err := st.writeParentReviewState(current); err != nil {
		t.Fatal(err)
	}
}

func TestParkedPlanRequiresDecisionOriginPendingDecision(t *testing.T) {
	st := newParkedPlanStore(t)
	parkFromStatus(t, st, TaskStatusWaitingDecision)
	if err := st.Remove("pending-decision"); err != nil {
		t.Fatal(err)
	}
	setParkedReviewLabel(t, st, string(packet.StatusNeedsSolDecision))

	_, err := st.ParentActionPlan()
	var inconsistency *LifecycleInconsistencyError
	if !errors.As(err, &inconsistency) {
		t.Fatalf("plan error = %v, want LifecycleInconsistencyError", err)
	}
}

func TestParkedPlanRequiresDecisionOriginReviewLabelScope(t *testing.T) {
	st := newParkedPlanStore(t)
	parkFromStatus(t, st, TaskStatusWaitingDecision)
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	setParkedReviewLabel(t, st, string(packet.StatusNeedsSolReview))

	_, err := st.ParentActionPlan()
	var inconsistency *LifecycleInconsistencyError
	if !errors.As(err, &inconsistency) {
		t.Fatalf("plan error = %v, want LifecycleInconsistencyError", err)
	}
}

func TestParkedPlanRequiresReviewOriginWithoutPendingDecision(t *testing.T) {
	st := newParkedPlanStore(t)
	parkFromStatus(t, st, TaskStatusWaitingSolReview)
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	setParkedReviewLabel(t, st, string(packet.StatusNeedsSolReview))

	_, err := st.ParentActionPlan()
	var inconsistency *LifecycleInconsistencyError
	if !errors.As(err, &inconsistency) {
		t.Fatalf("plan error = %v, want LifecycleInconsistencyError", err)
	}
}

func TestParkedPlanAcceptsConsistentOrigins(t *testing.T) {
	for _, tc := range []struct {
		from    TaskStatus
		pending bool
		label   string
	}{
		{TaskStatusWaitingDecision, true, string(packet.StatusNeedsSolDecision)},
		{TaskStatusWaitingDecision, true, roundCommentNone},
		{TaskStatusWaitingSolReview, false, string(packet.StatusNeedsSolReview)},
		{TaskStatusWaitingSolReview, false, roundCommentNone},
	} {
		st := newParkedPlanStore(t)
		if tc.pending {
			if err := st.Touch("pending-decision"); err != nil {
				t.Fatal(err)
			}
		}
		setParkedReviewLabel(t, st, tc.label)
		parkFromStatus(t, st, tc.from)

		plan, err := st.ParentActionPlan()
		if err != nil {
			t.Fatalf("from=%s pending=%v label=%s: %v", tc.from, tc.pending, tc.label, err)
		}
		if plan.RequiredAction != ParentActionUnpark || !plan.Allows(ParentActionUnpark) {
			t.Fatalf("from=%s plan = %#v", tc.from, plan)
		}
	}
}

func TestParkedPlanRejectsUnparkableParkRecordOrigin(t *testing.T) {
	st := newParkedPlanStore(t)
	parkFromStatus(t, st, TaskStatusWaitingSolReview)
	record, err := st.LoadParkRecord()
	if err != nil {
		t.Fatal(err)
	}
	record.FromStatus = TaskStatusActive
	if err := st.SaveParkRecord(record); err != nil {
		t.Fatal(err)
	}

	_, err = st.ParentActionPlan()
	var inconsistency *LifecycleInconsistencyError
	if !errors.As(err, &inconsistency) {
		t.Fatalf("plan error = %v, want LifecycleInconsistencyError", err)
	}
}
