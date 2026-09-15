package report

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func convergenceInternalBaseTime() time.Time {
	return time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
}

func TestConvergenceHighFloorReviewerDoesNotCreateSubsequentGap(t *testing.T) {
	base := convergenceInternalBaseTime()
	snapshot := state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"}
	records := []state.RoundRecord{
		{Seq: 1, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base, Snapshot: snapshot},
		{Seq: 2, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second), Snapshot: snapshot},
		{Seq: 3, ReviewNumber: 2, WorkerPhase: "worker-auto-fix-1", CapturedAt: base.Add(30 * time.Second), Snapshot: snapshot},
	}
	logs := []state.ModelCallLog{{
		CallType: state.CallTypeTask, Role: state.ReviewerRole, Phase: "reviewer-1-high-floor",
		StartedAt: base.Add(20 * time.Second), PacketStatus: "NEEDS_SOL_REVIEW",
	}}
	rounds, _ := BuildConvergenceRounds(records, logs)
	if len(rounds) != 2 {
		t.Fatalf("rounds = %#v", rounds)
	}
	if rounds[0].mismatch {
		t.Fatalf("high-floor reviewer was treated as mismatch: %#v", rounds[0])
	}
	if rounds[1].gap || rounds[1].delta.Class == state.RoundDeltaUnknown {
		t.Fatalf("high-floor reviewer created a subsequent gap: %#v", rounds[1])
	}
}

func TestConvergenceRiskFloorResultCorrectionRemainsRiskFloor(t *testing.T) {
	riskRound := ConvergenceRound{reviewer: []state.ModelCallLog{{
		Role: state.ReviewerRole, Phase: "reviewer-1-risk-floor-result-correct",
	}}}
	if got := ConvergenceReviewOutDetail(riskRound); !got.RiskFloorReemit {
		t.Fatalf("risk-floor result correction lost risk-floor identity: %#v", got)
	}
	highRound := ConvergenceRound{reviewer: []state.ModelCallLog{{
		Role: state.ReviewerRole, Phase: "reviewer-1-high-floor-result-correct",
	}}}
	if got := ConvergenceReviewOutDetail(highRound); got.RiskFloorReemit {
		t.Fatalf("high-floor result correction was conflated with risk-floor: %#v", got)
	}
}
