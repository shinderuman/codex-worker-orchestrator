package app

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewGapHistoricalCategoryUsesTaskActivationNotCurrentPin(t *testing.T) {
	active := true
	fix := reviewGapActivationFixture(t, &active, repositoryharness.ActivationInactiveValue)
	if fix.CategoryStatus != reviewGapKnown || len(fix.Categories) != 1 || fix.Categories[0] != state.FixCategoryMetadata {
		t.Fatalf("historical active task category = %#v", fix)
	}
}

func TestReviewGapMissingTaskActivationDoesNotBorrowCurrentPin(t *testing.T) {
	fix := reviewGapActivationFixture(t, nil, repositoryharness.ActivationActiveValue)
	if fix.CategoryStatus != reviewGapUnknown || fix.CategoryReason != reviewGapReasonRepositoryHarnessActivationMissing || len(fix.Categories) != 0 {
		t.Fatalf("missing task activation category = %#v", fix)
	}
}

func TestReviewGapHistoricalInactiveTaskStaysGenericWhenCurrentPinIsActive(t *testing.T) {
	inactive := false
	fix := reviewGapActivationFixture(t, &inactive, repositoryharness.ActivationActiveValue)
	if fix.CategoryStatus != reviewGapKnown || len(fix.Categories) != 1 || fix.Categories[0] != state.FixCategoryDocumentation {
		t.Fatalf("historical inactive task category = %#v", fix)
	}
}

func reviewGapActivationFixture(t *testing.T, taskActivation *bool, currentActivation string) reviewGapFix {
	t.Helper()
	cfg, st, _ := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if taskActivation != nil {
		if err := st.RecordRepositoryHarnessActivation(*taskActivation); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Write(repositoryharness.ActivationStateKey, currentActivation); err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC().Add(-time.Hour)
	fixAt := start.Add(10 * time.Minute)
	writeReviewGapTelemetry(t, st, taskID, []state.ModelCallLog{
		reviewGapParentEvent(taskID, fixAt, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginCodexReview, state.ParentCauseWorker),
	})
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
		stats.ModelCalls = 1
	})
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: start,
		Paths: []state.RoundPathState{{Path: state.ParentPlanFile, Class: "doc", FullDigest: "before"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, ReviewNumber: 1, WorkerPhase: state.WorkerPhaseCategoryExplicitFix, CapturedAt: fixAt.Add(time.Minute),
		Paths: []state.RoundPathState{{Path: state.ParentPlanFile, Class: "doc", FullDigest: "after"}},
	}); err != nil {
		t.Fatal(err)
	}

	report := runReviewGap(t, cfg, taskID)
	if len(report.Fixes) != 1 {
		t.Fatalf("review-gap fixes = %#v", report.Fixes)
	}
	return report.Fixes[0]
}
