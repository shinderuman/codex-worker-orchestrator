package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestSetTaskStatusRecordsLifecycleTransitionsOnChangeOnly(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	records, err := ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].From != string(TaskStatusNone) || records[0].To != string(TaskStatusActive) {
		t.Fatalf("first transition = %#v", records[0])
	}
	if records[1].From != string(TaskStatusActive) || records[1].To != string(TaskStatusWaitingDecision) {
		t.Fatalf("second transition = %#v", records[1])
	}
	if records[0].Timestamp.IsZero() || records[1].Timestamp.Before(records[0].Timestamp) {
		t.Fatalf("timestamps are not monotonic: %#v", records)
	}
	if records[0].TaskID != taskID || records[1].TaskID != taskID {
		t.Fatalf("task attribution = %#v", records)
	}
}

func TestSetTaskStatusSkipsLifecycleWithoutTaskIdentity(t *testing.T) {
	st := newLifecycleTestStore(t)
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Path("lifecycle")); !os.IsNotExist(err) {
		t.Fatalf("lifecycle log should not exist without a task: %v", err)
	}
}

func TestEnterStopQualityGateClearsResidualPendingDecision(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	completed := packet.Result{Status: packet.StatusImplemented}
	checkpoint := ResumeCheckpoint{
		Stage:              ResumeStageWorker,
		Phase:              "worker-decision",
		Role:               WorkerRole,
		Model:              "opus",
		Request:            "request",
		Decision:           "decision-body",
		StopKind:           ResumeStopQualityGate,
		QualityGateFailure: "quality tool version mismatch",
		CompletedResult:    &completed,
	}

	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	if st.Exists("pending-decision") {
		t.Fatal("quality-gate stopが残存pending-decisionを行き止まり状態として残しています")
	}
	if st.TaskStatus() != TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionRepairQualityGateThenResume || !plan.Allows(ParentActionResume) {
		t.Fatalf("recovery plan = %#v", plan)
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Decision != "decision-body" {
		t.Fatalf("checkpoint decision = %q", saved.Decision)
	}
}

func TestBeginParentDecisionRollbackRestoresWaitingDecision(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}

	rollback, err := st.BeginParentDecision()
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive || !st.Exists("pending-decision") {
		t.Fatalf("begin state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}

	cause := errors.New("repository instruction surface guard failed: before-call-mismatch")
	if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
		t.Fatalf("rollback error = %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != ParentActionDecision {
		t.Fatalf("rollback plan = %#v err=%v", plan, planErr)
	}
}

func TestBeginParentFixRollbackRestoresWaitingSolReview(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}

	rollback, err := st.BeginParentFix(ParentOriginGLMReviewer, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive || st.Exists("pending-decision") {
		t.Fatalf("begin state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}

	cause := errors.New("repository instruction surface guard failed: before-call-mismatch")
	if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
		t.Fatalf("rollback error = %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || !plan.Allows(ParentActionFix) {
		t.Fatalf("rollback plan = %#v err=%v", plan, planErr)
	}
}

func TestBeginParentDecisionInternalFailureRollsBack(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(st.Path("task.status"))
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	_, err := st.BeginParentDecision()
	if err == nil || !strings.Contains(err.Error(), "parent action begin failed and rollback failed") {
		t.Fatalf("begin failure error = %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("failed begin state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverParentActionBeginRestoresParentWaitingStates(t *testing.T) {
	decisionStore := newLifecycleTestStore(t)
	if _, err := decisionStore.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := decisionStore.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := decisionStore.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if _, err := decisionStore.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}

	if err := decisionStore.RecoverParentActionBegin(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if decisionStore.TaskStatus() != TaskStatusWaitingDecision || !decisionStore.Exists("pending-decision") {
		t.Fatalf("recovered decision state: status=%s pending=%t", decisionStore.TaskStatus(), decisionStore.Exists("pending-decision"))
	}
	plan, planErr := decisionStore.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != ParentActionDecision {
		t.Fatalf("recovered decision plan = %#v err=%v", plan, planErr)
	}
	if err := decisionStore.RecoverParentActionBegin(TaskStatusWaitingDecision); err == nil {
		t.Fatal("recovery must not run twice on the restored state")
	}

	fixStore := newLifecycleTestStore(t)
	if _, err := fixStore.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := fixStore.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if _, err := fixStore.BeginParentFix(ParentOriginCodexReview, ParentCauseParentOrchestration); err != nil {
		t.Fatal(err)
	}

	if err := fixStore.RecoverParentActionBegin(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if fixStore.TaskStatus() != TaskStatusWaitingSolReview || fixStore.Exists("pending-decision") {
		t.Fatalf("recovered fix state: status=%s pending=%t", fixStore.TaskStatus(), fixStore.Exists("pending-decision"))
	}
	fixPlan, fixPlanErr := fixStore.ParentActionPlan()
	if fixPlanErr != nil || !fixPlan.Allows(ParentActionFix) {
		t.Fatalf("recovered fix plan = %#v err=%v", fixPlan, fixPlanErr)
	}
}

func TestRecoverParentActionBeginRejectsLifecycleContradictions(t *testing.T) {
	tests := []struct {
		name   string
		target TaskStatus
		seed   func(t *testing.T, st *StateStore)
	}{
		{
			name:   "task is not the active leftover",
			target: TaskStatusWaitingDecision,
			seed: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "resume checkpoint is present",
			target: TaskStatusWaitingDecision,
			seed: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := ResumeCheckpoint{
					Stage:    ResumeStageWorker,
					Phase:    "worker-decision",
					Role:     WorkerRole,
					Model:    "opus",
					Request:  "request",
					StopKind: ResumeStopInterrupted,
				}
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "parent review is open",
			target: TaskStatusWaitingDecision,
			seed: func(t *testing.T, st *StateStore) {
				t.Helper()
				st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskLow}, ParentReviewProducer{})
			},
		},
		{
			name:   "decision target without the pending decision payload",
			target: TaskStatusWaitingDecision,
			seed: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := st.Remove("pending-decision"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "fix target with a pending decision",
			target: TaskStatusWaitingSolReview,
			seed: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := st.Remove("pending-decision"); err != nil {
					t.Fatal(err)
				}
				if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
					t.Fatal(err)
				}
				if _, err := st.BeginParentFix(ParentOriginCodexReview, ParentCauseParentOrchestration); err != nil {
					t.Fatal(err)
				}
				if err := st.Touch("pending-decision"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "target is not a parent waiting state",
			target: TaskStatusComplete,
			seed:   func(*testing.T, *StateStore) {},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLifecycleTestStore(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("last-request", "request"); err != nil {
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

			test.seed(t, st)

			before := st.TaskStatus()
			if err := st.RecoverParentActionBegin(test.target); err == nil {
				t.Fatal("contradictory lifecycle must be rejected")
			}
			if st.TaskStatus() != before {
				t.Fatalf("rejected recovery changed the status: %s want %s", st.TaskStatus(), before)
			}
		})
	}
}

func newLifecycleTestStore(t *testing.T) *StateStore {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("b", 64),
		StateBase: filepath.Join(root, "state"),
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
