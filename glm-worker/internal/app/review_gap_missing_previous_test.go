package app

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewGapCategoryUnknownWithoutPreviousRound(t *testing.T) {
	cfg, st, _ := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Hour)
	fixAt := start.Add(10 * time.Minute)
	writeReviewGapTelemetry(t, st, taskID, []state.ModelCallLog{
		reviewGapParentEvent(taskID, fixAt, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginCodexReview, state.ParentCauseWorker),
		reviewGapTaskCall(taskID, state.WorkerRole, reviewGapWorkerExplicitFixPhase, fixAt.Add(time.Minute), 1, 10),
	})
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
		stats.ModelCalls = 1
	})
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, ReviewNumber: 1,
		WorkerPhase: reviewGapWorkerExplicitFixPhase,
		CapturedAt:  fixAt.Add(2 * time.Minute),
		Paths: []state.RoundPathState{
			{Path: "glm-worker/internal/app/review_gap.go", Class: "code", FullDigest: "after"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	report := runReviewGap(t, cfg, taskID)
	if len(report.Fixes) != 1 {
		t.Fatalf("fixes = %d, want 1: %#v", len(report.Fixes), report.Fixes)
	}
	fix := report.Fixes[0]
	if fix.CategoryStatus != reviewGapUnknown || fix.CategoryReason != reviewGapReasonPreviousRoundMissing {
		t.Fatalf("category evidence = status %q reason %q, want unknown/%s", fix.CategoryStatus, fix.CategoryReason, reviewGapReasonPreviousRoundMissing)
	}
	if len(fix.Categories) != 0 {
		t.Fatalf("categories = %v, want none without previous round", fix.Categories)
	}
}
