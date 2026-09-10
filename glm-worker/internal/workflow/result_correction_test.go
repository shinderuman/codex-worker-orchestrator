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
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("terminal failure後にresult correction stateが残っています")
	}
	if _, loadErr := st.LoadResumeCheckpoint(); !errors.Is(loadErr, state.ErrNoResumeCheckpoint) {
		t.Fatalf("terminal failureがresume checkpointを残しています: %v", loadErr)
	}
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
			r := &scriptedRunner{steps: []runnerStep{
				{structured: constraintViolatingImplementedPacket()},
				{structured: multilineSummaryImplementedPacket()},
				{structured: tc.third},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err := w.runModel(baseCorrectionCheckpoint())
			failure, ok := ResultCorrectionFailureFromError(err)
			if !ok || failure.Reason != "budget_exhausted" || failure.Attempts != 2 {
				t.Fatalf("budget exhausted failureを期待: err=%v failure=%#v", err, failure)
			}
			if failure.TaskID == "" || failure.SessionID == "" || failure.Snapshot.Head == "" || len(failure.Violations) < 2 {
				t.Fatalf("terminal evidenceが不足しています: %#v", failure)
			}
			if len(r.prompts) != 3 {
				t.Fatalf("correction budgetを超えてmodel callしています: calls=%d", len(r.prompts))
			}
			if st.TaskStatus() != state.TaskStatusActive {
				t.Fatalf("terminal failure後status = %s", st.TaskStatus())
			}
			if _, loadErr := st.LoadResumeCheckpoint(); !errors.Is(loadErr, state.ErrNoResumeCheckpoint) {
				t.Fatalf("budget exhausted後にresume可能です: %v", loadErr)
			}
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
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("snapshot boundary failure後にresult correction stateが残っています")
	}
}
