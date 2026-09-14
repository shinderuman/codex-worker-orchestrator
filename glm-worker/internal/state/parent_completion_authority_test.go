package state

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentCompletionOutcomeSurvivesTelemetryRemovalAndConflictingStats(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}

	outcome, err := st.CurrentParentCompletionOutcome()
	if err != nil {
		t.Fatal(err)
	}
	if outcome == nil || outcome.Terminal != SessionRotationTerminalAccept || outcome.Risk != string(packet.RiskHigh) {
		t.Fatalf("canonical completion outcome = %#v", outcome)
	}

	st.UpdateTaskStats(func(stats *TaskStats) {
		stats.CompletionTerminal = SessionRotationTerminalNoGo
		stats.AcceptedRisk = string(packet.RiskLow)
	})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.ModelCallLogPath(taskID)); err != nil {
		t.Fatal(err)
	}

	completed, err := st.CompleteParentAwaiting(nil)
	if err != nil || !completed {
		t.Fatalf("complete without telemetry = %v err=%v", completed, err)
	}
	if st.TaskStatus() != TaskStatusComplete {
		t.Fatalf("completion status = %s", st.TaskStatus())
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CompletionTerminal != SessionRotationTerminalAccept || stats.AcceptedRisk != string(packet.RiskHigh) {
		t.Fatalf("canonical outcome was not projected back to stats: terminal=%q risk=%q", stats.CompletionTerminal, stats.AcceptedRisk)
	}
}

func TestAcceptParentReviewDoesNotRequireWritableTaskStats(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.Path(currentStatsFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.Path(currentStatsFile), 0o700); err != nil {
		t.Fatal(err)
	}

	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept with unavailable stats mirror = %v err=%v", accepted, err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("accept status = %s", st.TaskStatus())
	}
	outcome, err := st.CurrentParentCompletionOutcome()
	if err != nil || outcome == nil || outcome.Terminal != SessionRotationTerminalAccept || outcome.Risk != string(packet.RiskLow) {
		t.Fatalf("canonical completion outcome = %#v err=%v", outcome, err)
	}
	info, err := os.Stat(st.Path(currentStatsFile))
	if err != nil || !info.IsDir() {
		t.Fatalf("observational stats fixture changed: info=%v err=%v", info, err)
	}
}
