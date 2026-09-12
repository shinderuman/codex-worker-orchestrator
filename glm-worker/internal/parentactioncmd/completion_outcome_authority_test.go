package parentactioncmd

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompleteIgnoresConflictingTaskStatsCompletionOutcome(t *testing.T) {
	fixture := newCompleteRepositoryFixture(t)
	if err := fixture.st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	fixture.st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.CompletionTerminal = state.SessionRotationTerminalNoGo
		stats.AcceptedRisk = string(packet.RiskLow)
	})

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("conflicting stats後のoutput = %#v", output)
	}
	stats, err := fixture.st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CompletionTerminal != state.SessionRotationTerminalAccept || stats.AcceptedRisk != string(packet.RiskHigh) {
		t.Fatalf("canonical outcomeがmirrorへ反映されていません: terminal=%q risk=%q", stats.CompletionTerminal, stats.AcceptedRisk)
	}
	marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID)
	if err != nil || marker == nil || marker.LastEvaluation == nil {
		t.Fatalf("rotation marker = %#v err=%v", marker, err)
	}
	if marker.LastEvaluation.Terminal != state.SessionRotationTerminalAccept || !marker.LastEvaluation.Required || marker.LastEvaluation.Reason != state.SessionRotationReasonHighRisk {
		t.Fatalf("canonical accepted riskがrotation評価へ届いていません: %#v", marker.LastEvaluation)
	}
}

func TestCompleteRecoversMissingTaskStatsFromCanonicalOutcome(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	if err := os.Remove(fixture.st.Path("task-stats.json")); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("missing stats後のoutput = %#v", output)
	}
	stats, err := fixture.st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CompletionTerminal != state.SessionRotationTerminalAccept || stats.AcceptedRisk != string(packet.RiskLow) {
		t.Fatalf("canonical outcome mirror = terminal:%q risk:%q", stats.CompletionTerminal, stats.AcceptedRisk)
	}
}
