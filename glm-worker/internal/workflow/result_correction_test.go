package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func multilineSummaryImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "line one\nline two",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func missingTestsImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "done",
		RequirementCoverage: "covered",
		Unverified:          "none",
	})
}

func baseCorrectionCheckpoint() state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original",
		OriginalPrompt: "original",
		Request:        "request",
	}
}

func TestRunModelUsesSecondCorrectionOnlyForNewViolation(t *testing.T) {
	st := newStateStoreT(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: multilineSummaryImplementedPacket()},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(baseCorrectionCheckpoint())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusImplemented {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("model calls = %d want 3", len(r.prompts))
	}
	if r.phases[1] != "worker-new"+resultCorrectionPhaseSuffix || r.phases[2] != "worker-new"+resultCorrectionPhaseSuffix+resultCorrectionPhaseSuffix {
		t.Fatalf("correction phases = %#v", r.phases)
	}
	if !strings.Contains(r.prompts[2], "field summaryに改行") {
		t.Fatalf("2回目の補正promptに新規違反がありません: %s", r.prompts[2])
	}
	afterTaskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	afterSessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	if afterTaskID != taskID || afterSessionID != sessionID {
		t.Fatalf("correction changed lifecycle identity: task=%q/%q session=%q/%q", taskID, afterTaskID, sessionID, afterSessionID)
	}
	stats := currentStats(t, st)
	if stats.ModelCalls != 3 || stats.ResultCorrections != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("成功後にresult correction stateが残っています")
	}
}

func TestRunModelStopsOnRepeatedFirstCorrectionViolation(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" || failure.Attempts != 1 {
		t.Fatalf("repeated violation terminal failureを期待: err=%v failure=%#v", err, failure)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("同一違反へ追加補正を実行しています: calls=%d", len(r.prompts))
	}
	assertNoCorrectionRecoveryAction(t, st)
	assertTerminalCorrectionState(t, w, "repeated_violation")
}

func TestRunModelCorrectionBudgetExhaustionIsTerminal(t *testing.T) {
	tests := []struct {
		name  string
		third string
	}{
		{name: "second correction repeats violation", third: multilineSummaryImplementedPacket()},
		{name: "different violation remains after second correction", third: missingTestsImplementedPacket()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newStateStoreT(t)
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			r := &scriptedRunner{steps: []runnerStep{
				{structured: constraintViolatingImplementedPacket()},
				{structured: multilineSummaryImplementedPacket()},
				{structured: tc.third},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err = w.runModel(baseCorrectionCheckpoint())
			failure, ok := ResultCorrectionFailureFromError(err)
			if !ok || failure.Reason != "budget_exhausted" || failure.Attempts != 2 {
				t.Fatalf("budget exhausted failureを期待: err=%v failure=%#v", err, failure)
			}
			if failure.TaskID != taskID || failure.SessionID == "" || failure.Snapshot.Head == "" || len(failure.Violations) < 2 {
				t.Fatalf("terminal evidenceが不足しています: %#v", failure)
			}
			if len(r.prompts) != 3 {
				t.Fatalf("correction budgetを超えてmodel callしています: calls=%d", len(r.prompts))
			}
			if st.TaskStatus() != state.TaskStatusActive {
				t.Fatalf("terminal failure後status = %s", st.TaskStatus())
			}
			assertNoCorrectionRecoveryAction(t, st)
			assertTerminalCorrectionState(t, w, "budget_exhausted")
		})
	}
}

func TestRunModelCorrectionFailsClosedOnSnapshotChange(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	captures := 0
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		captures++
		if captures >= 3 {
			changed := fixedSnapshot
			changed.WorktreeDigest = "changed-worktree"
			return changed, nil
		}
		return fixedSnapshot, nil
	}

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "boundary_changed" || failure.BoundaryMismatch != "repository snapshot changed" {
		t.Fatalf("snapshot boundary failureを期待: err=%v failure=%#v", err, failure)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("snapshot変更後に追加model callしています: calls=%d", len(r.prompts))
	}
	assertNoCorrectionRecoveryAction(t, st)
	assertTerminalCorrectionState(t, w, "boundary_changed")
}

func TestTerminalCorrectionStateBlocksFreshModelAdmission(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacket("must-not-run")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if _, err := w.runModel(baseCorrectionCheckpoint()); err == nil {
		t.Fatal("terminal correction failureを期待")
	}
	if _, err := w.runModel(baseCorrectionCheckpoint()); err == nil {
		t.Fatal("terminal correction stateがfresh model callを許可しました")
	}
	if len(r.prompts) != 2 {
		t.Fatalf("terminal latch後にmodel callが実行されました: %d", len(r.prompts))
	}
}

func assertTerminalCorrectionState(t *testing.T, w *Workflow, reason string) {
	t.Helper()
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Terminal == nil || record.Terminal.Reason != reason {
		t.Fatalf("terminal correction state = %#v", record)
	}
}

func assertNoCorrectionRecoveryAction(t *testing.T, st *state.StateStore) {
	t.Helper()
	if _, loadErr := st.LoadResumeCheckpoint(); !errors.Is(loadErr, state.ErrNoResumeCheckpoint) {
		t.Fatalf("terminal correctionがresume checkpointを残しています: %v", loadErr)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionNone || plan.Allows(state.ParentActionResume) {
		t.Fatalf("terminal correctionがparent recovery actionを要求しています: %#v", plan)
	}
}
