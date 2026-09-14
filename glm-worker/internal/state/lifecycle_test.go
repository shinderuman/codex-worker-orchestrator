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
	if _, err := st.loadParentActionBegin(); err != nil {
		t.Fatalf("begin record = %v", err)
	}

	cause := errors.New("repository instruction surface guard failed: before-call-mismatch")
	if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
		t.Fatalf("rollback error = %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if _, err := st.loadParentActionBegin(); !errors.Is(err, errNoParentActionBegin) {
		t.Fatalf("rollback left begin record: %v", err)
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
	if _, err := st.loadParentActionBegin(); err != nil {
		t.Fatalf("begin record = %v", err)
	}

	cause := errors.New("repository instruction surface guard failed: before-call-mismatch")
	if err := st.RollbackParentAction(rollback, cause); !errors.Is(err, cause) {
		t.Fatalf("rollback error = %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if _, err := st.loadParentActionBegin(); !errors.Is(err, errNoParentActionBegin) {
		t.Fatalf("rollback left begin record: %v", err)
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || !plan.Allows(ParentActionFix) {
		t.Fatalf("rollback plan = %#v err=%v", plan, planErr)
	}
}

func TestBeginParentDecisionInternalFailureLeavesWaitingState(t *testing.T) {
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

	if _, err := st.BeginParentDecision(); err == nil {
		t.Fatal("begin must fail when canonical begin state cannot be persisted")
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("failed begin state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverParentActionBeginFromStateRestoresParentWaitingStates(t *testing.T) {
	decisionStore := newDecisionBeginRecoveryStore(t)

	target, err := decisionStore.RecoverParentActionBeginFromState()
	if err != nil {
		t.Fatal(err)
	}
	if target != TaskStatusWaitingDecision {
		t.Fatalf("recovered target = %s", target)
	}
	if decisionStore.TaskStatus() != TaskStatusWaitingDecision || !decisionStore.Exists("pending-decision") {
		t.Fatalf("recovered decision state: status=%s pending=%t", decisionStore.TaskStatus(), decisionStore.Exists("pending-decision"))
	}
	plan, planErr := decisionStore.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != ParentActionDecision {
		t.Fatalf("recovered decision plan = %#v err=%v", plan, planErr)
	}
	if _, err := decisionStore.RecoverParentActionBeginFromState(); !errors.Is(err, errNoParentActionBegin) {
		t.Fatalf("second recovery error = %v", err)
	}

	fixStore := newFixBeginRecoveryStore(t)
	target, err = fixStore.RecoverParentActionBeginFromState()
	if err != nil {
		t.Fatal(err)
	}
	if target != TaskStatusWaitingSolReview {
		t.Fatalf("recovered fix target = %s", target)
	}
	if fixStore.TaskStatus() != TaskStatusWaitingSolReview || fixStore.Exists("pending-decision") {
		t.Fatalf("recovered fix state: status=%s pending=%t", fixStore.TaskStatus(), fixStore.Exists("pending-decision"))
	}
	fixPlan, fixPlanErr := fixStore.ParentActionPlan()
	if fixPlanErr != nil || !fixPlan.Allows(ParentActionFix) {
		t.Fatalf("recovered fix plan = %#v err=%v", fixPlan, fixPlanErr)
	}
}

func TestRecoverParentActionBeginFromStateClearsMatchingPreCallCheckpoint(t *testing.T) {
	tests := []struct {
		name   string
		seed   func(t *testing.T) *StateStore
		phase  string
		target TaskStatus
	}{
		{name: "decision", seed: newDecisionBeginRecoveryStore, phase: "worker-decision", target: TaskStatusWaitingDecision},
		{name: "fix", seed: newFixBeginRecoveryStore, phase: "worker-explicit-fix", target: TaskStatusWaitingSolReview},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := test.seed(t)
			checkpoint := ResumeCheckpoint{
				Stage:   ResumeStageWorker,
				Phase:   test.phase,
				Role:    WorkerRole,
				Model:   "opus",
				Request: "request",
			}
			if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
				t.Fatal(err)
			}
			target, err := st.RecoverParentActionBeginFromState()
			if err != nil {
				t.Fatal(err)
			}
			if target != test.target || st.TaskStatus() != test.target {
				t.Fatalf("recovered state: target=%s status=%s want=%s", target, st.TaskStatus(), test.target)
			}
			if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, ErrNoResumeCheckpoint) {
				t.Fatalf("pre-call checkpoint remains after recovery: %v", err)
			}
		})
	}
}

func TestCommitParentActionBeginEndsRecoveryWindow(t *testing.T) {
	st := newDecisionBeginRecoveryStore(t)
	if err := st.CommitParentActionBegin(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if _, err := st.RecoverParentActionBeginFromState(); !errors.Is(err, errNoParentActionBegin) {
		t.Fatalf("recovery remains available after admission commit: %v", err)
	}
}

func TestRecoverParentActionBeginFromStateRejectsLifecycleContradictions(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T) *StateStore
	}{
		{
			name: "canonical begin record is missing",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				if err := st.Remove(parentActionBeginStateFile); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "task identity changed",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				if err := st.Write("task.id", "different-task"); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "task is no longer active or the recorded source",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "stopped resume checkpoint is present",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
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
				return st
			},
		},
		{
			name: "pre-call checkpoint phase does not match source",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				checkpoint := ResumeCheckpoint{
					Stage:   ResumeStageWorker,
					Phase:   "worker-explicit-fix",
					Role:    WorkerRole,
					Model:   "opus",
					Request: "request",
				}
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "parent review is open",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskLow}, ParentReviewProducer{}); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "decision source lost its pending payload",
			seed: func(t *testing.T) *StateStore {
				st := newDecisionBeginRecoveryStore(t)
				if err := st.Remove("pending-decision"); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
		{
			name: "fix source acquired a pending decision",
			seed: func(t *testing.T) *StateStore {
				st := newFixBeginRecoveryStore(t)
				if err := st.Touch("pending-decision"); err != nil {
					t.Fatal(err)
				}
				return st
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := test.seed(t)
			before := st.TaskStatus()
			if _, err := st.RecoverParentActionBeginFromState(); err == nil {
				t.Fatal("contradictory lifecycle must be rejected")
			}
			if st.TaskStatus() != before {
				t.Fatalf("rejected recovery changed the status: %s want %s", st.TaskStatus(), before)
			}
		})
	}
}

func newDecisionBeginRecoveryStore(t *testing.T) *StateStore {
	t.Helper()
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
	return st
}

func newFixBeginRecoveryStore(t *testing.T) *StateStore {
	t.Helper()
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(ParentOriginCodexReview, ParentCauseParentOrchestration); err != nil {
		t.Fatal(err)
	}
	return st
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
