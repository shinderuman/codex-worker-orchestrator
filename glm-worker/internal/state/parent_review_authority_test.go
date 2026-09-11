package state

import (
	"errors"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentReviewAuthoritySurvivesTaskStatsCorruption(t *testing.T) {
	st := newParentReviewAuthorityStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: string(ReviewerRole), Model: "sonnet"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(currentStatsFile), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	label, err := st.CurrentParentReviewLabel()
	if err != nil || label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("canonical review = %q err=%v", label, err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatalf("task-stats corruption changed live admission: %v", err)
	}
	if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionAccept) || !plan.Allows(ParentActionFix) {
		t.Fatalf("review plan = %#v", plan)
	}
}

func TestParentReviewAuthorityIgnoresConflictingTaskStatsMirror(t *testing.T) {
	st := newParentReviewAuthorityStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ParentReviewOpen = &ParentReviewOpenState{PacketStatus: string(packet.StatusPass), Risk: string(packet.RiskLow)}
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionAccept) || !plan.Allows(ParentActionFix) {
		t.Fatalf("conflicting stats mirror overrode canonical review: %#v", plan)
	}
	label, err := st.CurrentParentReviewLabel()
	if err != nil || label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("canonical label = %q err=%v", label, err)
	}
}

func TestParentReviewAuthorityMissingOrCorruptFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *StateStore)
	}{
		{
			name: "missing",
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := os.Remove(st.Path(parentReviewStateFile)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "corrupt",
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := os.WriteFile(st.Path(parentReviewStateFile), []byte("{\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newParentReviewAuthorityStore(t)
			if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
				t.Fatal(err)
			}
			if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{}); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, st)

			if _, admitted, err := st.AdmitParentAction(ParentActionAccept); err == nil || admitted {
				t.Fatalf("accept did not fail closed: admitted=%v err=%v", admitted, err)
			}
			if _, admitted, err := st.AdmitNewTask(); err == nil || admitted {
				t.Fatalf("new-task did not fail closed: admitted=%v err=%v", admitted, err)
			}
			if accepted, err := st.AcceptParentReview(); err == nil || accepted {
				t.Fatalf("direct accept did not fail closed: accepted=%v err=%v", accepted, err)
			}
		})
	}
}

func TestParentReviewAuthorityRollbackRestoresReview(t *testing.T) {
	t.Run("fix", func(t *testing.T) {
		st := newParentReviewAuthorityStore(t)
		if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}
		rollback, err := st.BeginParentFix(ParentOriginCodexReview, ParentCauseWorker)
		if err != nil {
			t.Fatal(err)
		}
		assertParentReviewLabel(t, st, roundCommentNone)
		cause := errors.New("injected post-begin failure")
		if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
			t.Fatalf("rollback error = %v", err)
		}
		assertParentReviewLabel(t, st, string(packet.StatusNeedsSolReview))
		if st.TaskStatus() != TaskStatusWaitingSolReview {
			t.Fatalf("status after rollback = %s", st.TaskStatus())
		}
	})

	t.Run("decision", func(t *testing.T) {
		st := newParentReviewAuthorityStore(t)
		if err := st.WaitForDecision(); err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}
		rollback, err := st.BeginParentDecision()
		if err != nil {
			t.Fatal(err)
		}
		assertParentReviewLabel(t, st, roundCommentNone)
		cause := errors.New("injected post-begin failure")
		if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
			t.Fatalf("rollback error = %v", err)
		}
		assertParentReviewLabel(t, st, string(packet.StatusNeedsSolDecision))
		if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
			t.Fatalf("decision state after rollback = status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
		}
	})
}

func TestParentReviewAuthorityNewTaskGetsFreshCanonicalState(t *testing.T) {
	st := newParentReviewAuthorityStore(t)
	firstTask, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{Role: string(ReviewerRole), Model: "haiku"}); err != nil {
		t.Fatal(err)
	}
	secondTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if secondTask == firstTask {
		t.Fatalf("task identity did not change: %s", firstTask)
	}
	assertParentReviewLabel(t, st, roundCommentNone)
	state, err := st.loadParentReviewState()
	if err != nil {
		t.Fatal(err)
	}
	if state.TaskID != secondTask || state.Open != nil {
		t.Fatalf("new task parent review state = %#v", state)
	}

	evidence, err := st.ArchivedTaskStatsEvidence(firstTask)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Proven || evidence.TaskID != firstTask {
		t.Fatalf("archived stats evidence = %#v", evidence)
	}
}

func newParentReviewAuthorityStore(t *testing.T) *StateStore {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}

func assertParentReviewLabel(t *testing.T, st *StateStore, want string) {
	t.Helper()
	got, err := st.CurrentParentReviewLabel()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("parent review label = %q want %q", got, want)
	}
}
