package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeReviewGapTelemetry(t *testing.T, st *state.StateStore, taskID string, records []state.ModelCallLog) {
	t.Helper()
	path := st.ModelCallLogPath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, data...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
}

func reviewGapTaskCall(taskID string, role state.SessionRole, phase string, at time.Time, turns int, input int64) state.ModelCallLog {
	return state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, TaskID: taskID,
		CallID: "call-" + phase + "-" + at.Format(time.RFC3339Nano), SessionID: "session-" + taskID,
		StartedAt: at, CompletedAt: at.Add(time.Minute), Phase: phase, Role: role,
		ModelAlias: "opus", Outcome: "success",
		TopLevelUsage: state.TokenUsage{InputTokens: input, OutputTokens: input / 2},
		TreeUsage:     state.TokenUsage{InputTokens: input, OutputTokens: input / 2},
		TopLevelTurns: turns, WallDurationMS: 1000,
	}
}

func reviewGapParentEvent(taskID string, at time.Time, phase, outcome, origin, cause string) state.ModelCallLog {
	return state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeEvent, TaskID: taskID,
		CallID:    "event-" + outcome + "-" + at.Format(time.RFC3339Nano),
		StartedAt: at, CompletedAt: at, Phase: phase, Role: state.WorkerRole,
		Outcome: outcome, ParentOrigin: origin, ParentCause: cause,
	}
}

func runReviewGap(t *testing.T, cfg config.AppConfig, taskID string) reviewGapReport {
	t.Helper()
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeReviewGap, Payload: taskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(stdout.Bytes(), []byte("\n{")) != 0 {
		t.Fatalf("review gap stdout is not a single JSON object: %s", stdout.String())
	}
	var report reviewGapReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func newReviewGapCorrelatedFixture(t *testing.T) (config.AppConfig, string, reviewGapReport) {
	t.Helper()
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-2 * time.Hour)
	fix1At := start.Add(20 * time.Minute)
	fix2At := start.Add(40 * time.Minute)
	acceptAt := start.Add(50 * time.Minute)
	completeAt := start.Add(55 * time.Minute)

	writeAnalysisRollout(t, codexHome, analysisRolloutRel(), codexTestParentThreadID, start.Add(-3*time.Hour), []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 1000, 500, 100, 50, 1500),
		parentUsageTokenCountLine(t, fix1At.Add(-30*time.Second), 1300, 600, 150, 60, 2000),
		parentUsageTokenCountLine(t, fix2At.Add(-30*time.Second), 1600, 700, 200, 70, 2500),
		parentUsageTokenCountLine(t, acceptAt.Add(-30*time.Second), 1800, 800, 250, 80, 3000),
	})

	records := []state.ModelCallLog{
		reviewGapTaskCall(taskID, state.WorkerRole, "worker-new", start.Add(5*time.Minute), 3, 100),
		reviewGapTaskCall(taskID, state.ReviewerRole, "reviewer-1", start.Add(10*time.Minute), 1, 50),
		reviewGapParentEvent(taskID, fix1At, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginCodexReview, state.ParentCauseWorker),
		reviewGapTaskCall(taskID, state.WorkerRole, "worker-explicit-fix", fix1At.Add(time.Minute), 4, 200),
		reviewGapTaskCall(taskID, state.ReviewerRole, "reviewer-1", fix1At.Add(5*time.Minute), 2, 80),
		reviewGapParentEvent(taskID, fix2At, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginGLMReviewer, ""),
		reviewGapTaskCall(taskID, state.WorkerRole, "worker-explicit-fix", fix2At.Add(time.Minute), 1, 10),
		reviewGapTaskCall(taskID, state.ReviewerRole, "reviewer-1", fix2At.Add(2*time.Minute), 1, 5),
		reviewGapParentEvent(taskID, acceptAt, state.ParentPhaseAccept, state.ParentOutcomeAccepted, "", ""),
	}
	writeReviewGapTelemetry(t, st, taskID, records)
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
		stats.ModelCalls = 6
	})
	analysisRetireCurrentTask(t, st, taskID, start, completeAt)
	appendReviewGapFixtureRounds(t, st, taskID, start, fix1At, fix2At)

	return cfg, taskID, runReviewGap(t, cfg, "")
}

func appendReviewGapFixtureRounds(t *testing.T, st *state.StateStore, taskID string, start, fix1At, fix2At time.Time) {
	t.Helper()
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: start,
		Paths: []state.RoundPathState{{Path: "glm-worker/internal/app/app.go", Class: "code", FullDigest: "a1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-explicit-fix", CapturedAt: fix1At.Add(2 * time.Minute),
		Paths: []state.RoundPathState{
			{Path: "glm-worker/internal/app/app.go", Class: "code", FullDigest: "a1"},
			{Path: "glm-worker/internal/app/review_gap.go", Class: "code", FullDigest: "b2"},
			{Path: "glm-worker/internal/app/review_gap_test.go", Class: "code", FullDigest: "c3"},
			{Path: "codex/instructions/glm-execution.md", Class: "doc", FullDigest: "d4"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-explicit-fix", CapturedAt: fix2At.Add(3 * time.Minute),
		Paths: []state.RoundPathState{
			{Path: "glm-worker/internal/app/app.go", Class: "code", FullDigest: "a1"},
			{Path: "glm-worker/internal/app/review_gap.go", Class: "code", FullDigest: "b2"},
			{Path: "glm-worker/internal/app/review_gap_test.go", Class: "code", FullDigest: "c3"},
			{Path: "codex/instructions/glm-execution.md", Class: "doc", FullDigest: "d4"},
			{Path: "README.md", Class: "doc", FullDigest: "e5"},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewGapCorrelatesDeclarationRoundAndCategories(t *testing.T) {
	_, _, report := newReviewGapCorrelatedFixture(t)
	if report.Version != reviewGapReportVersion || report.Scope != reviewGapScopeAll ||
		report.TaskCount != 1 || report.FixCount != 2 || report.Coverage != reviewGapCoverageComplete {
		t.Fatalf("report header = %#v", report)
	}
	if len(report.Fixes) != 2 {
		t.Fatalf("fixes = %#v", report.Fixes)
	}

	first := report.Fixes[0]
	if first.Origin != state.ParentOriginCodexReview || first.Cause != state.ParentCauseWorker || first.CauseStatus != reviewGapKnown {
		t.Fatalf("first fix declaration = %#v", first)
	}
	if first.Round == nil || first.Round.Seq != 2 || first.Round.ReviewNumber != 1 || first.Round.WorkerPhase != "worker-explicit-fix" {
		t.Fatalf("first fix round = %#v", first.Round)
	}
	if first.CategoryStatus != reviewGapKnown || len(first.Categories) != 3 ||
		first.Categories[0] != state.FixCategoryInstruction || first.Categories[1] != state.FixCategoryProduction || first.Categories[2] != state.FixCategoryTest {
		t.Fatalf("first fix categories = %#v", first.Categories)
	}
	if first.SemanticityStatus != reviewGapKnown || first.Semanticity != state.RoundDeltaSemantic {
		t.Fatalf("first fix semanticity = %q status=%q", first.Semanticity, first.SemanticityStatus)
	}

	second := report.Fixes[1]
	if second.Origin != state.ParentOriginGLMReviewer || second.Cause != "" || second.CauseStatus != state.CauseStatusNotDeclared {
		t.Fatalf("second fix declaration = %#v", second)
	}
	if second.Round == nil || second.Round.Seq != 3 {
		t.Fatalf("second fix round = %#v", second.Round)
	}
	if second.CategoryStatus != reviewGapKnown || len(second.Categories) != 1 || second.Categories[0] != state.FixCategoryDocumentation {
		t.Fatalf("second fix categories = %#v", second.Categories)
	}
	if second.Semanticity != state.RoundDeltaDocChange {
		t.Fatalf("second fix semanticity = %q", second.Semanticity)
	}
}

func TestReviewGapCorrelatesParentReworkInterval(t *testing.T) {
	_, _, report := newReviewGapCorrelatedFixture(t)
	interval := report.Fixes[0].ParentReworkInterval
	if interval.From.Basis != reviewGapBasisTaskStart || interval.To.Basis != reviewGapBasisFixDeclaration {
		t.Fatalf("first interval boundaries = %#v -> %#v", interval.From, interval.To)
	}
	if interval.Status != analysisStatusAvailable || interval.Tokens.Status != analysisStatusAvailable ||
		interval.Tokens.InputTokens != 300 || interval.Tokens.CachedInputTokens != 100 ||
		interval.Tokens.OutputTokens != 50 || interval.Tokens.TotalTokens != 500 {
		t.Fatalf("first interval tokens = %#v", interval.Tokens)
	}
	if interval.Tokens.BaselineSource == "" || interval.Tokens.EndSource == "" {
		t.Fatalf("first interval token sources missing: %#v", interval.Tokens)
	}
	second := report.Fixes[1].ParentReworkInterval
	if second.From.Basis != reviewGapBasisPreviousOutcome || second.From.Phase != state.ParentPhaseFix {
		t.Fatalf("second interval from = %#v", second.From)
	}
	if second.Tokens.InputTokens != 300 {
		t.Fatalf("second interval tokens = %#v", second.Tokens)
	}
}

func TestReviewGapCorrelatesDownstreamCalls(t *testing.T) {
	_, _, report := newReviewGapCorrelatedFixture(t)
	downstream := report.Fixes[0].Downstream
	if downstream.Status != reviewGapKnown || len(downstream.WorkerCalls) != 1 || len(downstream.ReviewerCalls) != 1 {
		t.Fatalf("first downstream = %#v", downstream)
	}
	if downstream.WorkerCalls[0].Phase != "worker-explicit-fix" || downstream.ReviewerCalls[0].ReviewNumber != 1 {
		t.Fatalf("first downstream identities = %#v / %#v", downstream.WorkerCalls[0], downstream.ReviewerCalls[0])
	}
	if downstream.Rework == nil || downstream.Rework.Turns != 6 || downstream.Rework.TreeInputTokens != 280 ||
		downstream.Rework.TreeOutputTokens != 140 || downstream.Rework.WallDurationMS != 2000 {
		t.Fatalf("first downstream rework = %#v", downstream.Rework)
	}
}

func TestReviewGapSummaryCountsAndTaskScope(t *testing.T) {
	cfg, taskID, report := newReviewGapCorrelatedFixture(t)
	summary := report.Summary
	if summary.ByOrigin[state.ParentOriginCodexReview] != 1 || summary.ByOrigin[state.ParentOriginGLMReviewer] != 1 ||
		summary.ByCause[state.ParentCauseWorker] != 1 || len(summary.ByCause) != 1 ||
		summary.ByCauseStatus[reviewGapKnown] != 1 || summary.ByCauseStatus[state.CauseStatusNotDeclared] != 1 ||
		summary.ByCategory[state.FixCategoryInstruction] != 1 || summary.ByCategory[state.FixCategoryProduction] != 1 ||
		summary.ByCategory[state.FixCategoryTest] != 1 || summary.ByCategory[state.FixCategoryDocumentation] != 1 ||
		summary.BySemanticity[state.RoundDeltaSemantic] != 1 || summary.BySemanticity[state.RoundDeltaDocChange] != 1 {
		t.Fatalf("summary counts = %#v", summary)
	}

	scoped := runReviewGap(t, cfg, taskID)
	if scoped.Scope != reviewGapScopeTask || scoped.TaskID != taskID || scoped.FixCount != 2 {
		t.Fatalf("scoped report = %#v", scoped)
	}
}

func TestReviewGapPreservesFixCountWithUnknownEvidence(t *testing.T) {
	cfg, st, _ := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-1 * time.Hour)
	legacyAt := start.Add(10 * time.Minute)
	undeclaredAt := start.Add(30 * time.Minute)
	records := []state.ModelCallLog{
		reviewGapParentEvent(taskID, legacyAt, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginCodexReview, ""),
		reviewGapParentEvent(taskID, undeclaredAt, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginExternalReview, ""),
	}
	writeReviewGapTelemetry(t, st, taskID, records)
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
	})
	analysisRetireCurrentTask(t, st, taskID, start, start.Add(40*time.Minute))

	report := runReviewGap(t, cfg, "")
	if report.FixCount != 2 || len(report.Fixes) != 2 || report.Coverage != reviewGapCoverageComplete {
		t.Fatalf("fix count lost: %#v", report)
	}
	legacy := report.Fixes[0]
	if legacy.CauseStatus != state.CauseStatusMissingLegacy {
		t.Fatalf("legacy codex-review cause status = %q", legacy.CauseStatus)
	}
	if legacy.CategoryStatus != reviewGapUnknown || legacy.CategoryReason != reviewGapReasonRoundLogMissing ||
		legacy.SemanticityStatus != reviewGapUnknown || legacy.SemanticityReason != reviewGapReasonRoundLogMissing {
		t.Fatalf("legacy round evidence = %#v", legacy)
	}
	if legacy.Downstream.Status != reviewGapUnknown || legacy.Downstream.Reason != reviewGapReasonWorkerFixCallMissing {
		t.Fatalf("legacy downstream = %#v", legacy.Downstream)
	}
	if legacy.ParentReworkInterval.Status != codexStatusMissing ||
		legacy.ParentReworkInterval.Reason != "parent-session-missing" {
		t.Fatalf("legacy interval = %#v", legacy.ParentReworkInterval)
	}
	undeclared := report.Fixes[1]
	if undeclared.CauseStatus != state.CauseStatusNotDeclared {
		t.Fatalf("external-review cause status = %q", undeclared.CauseStatus)
	}
	if report.Summary.ByCauseStatus[state.CauseStatusMissingLegacy] != 1 ||
		report.Summary.ByCauseStatus[state.CauseStatusNotDeclared] != 1 || len(report.Summary.ByCause) != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestReviewGapCounterResetNotSummed(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-1 * time.Hour)
	fixAt := start.Add(10 * time.Minute)
	writeAnalysisRollout(t, codexHome, analysisRolloutRel(), codexTestParentThreadID, start.Add(-3*time.Hour), []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 2000, 1000, 200, 100, 3000),
		parentUsageTokenCountLine(t, fixAt.Add(-time.Second), 500, 100, 50, 20, 600),
	})
	writeReviewGapTelemetry(t, st, taskID, []state.ModelCallLog{
		reviewGapParentEvent(taskID, fixAt, state.ParentPhaseFix, state.ParentOutcomeFix, state.ParentOriginCodexReview, state.ParentCauseUnknown),
	})
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
	})
	analysisRetireCurrentTask(t, st, taskID, start, start.Add(20*time.Minute))

	report := runReviewGap(t, cfg, "")
	interval := report.Fixes[0].ParentReworkInterval
	if interval.Tokens.Status != analysisStatusCounterReset {
		t.Fatalf("counter reset tokens = %#v", interval.Tokens)
	}
	if interval.Tokens.InputTokens != 0 || interval.Tokens.BaselineSource == "" || interval.Tokens.EndSource == "" {
		t.Fatalf("counter reset must not sum and must keep sources: %#v", interval.Tokens)
	}
	if report.Fixes[0].Cause != state.ParentCauseUnknown || report.Fixes[0].CauseStatus != reviewGapKnown {
		t.Fatalf("explicit unknown cause = %#v", report.Fixes[0])
	}
}

func TestReviewGapMissingTaskFailsClosed(t *testing.T) {
	cfg, st, _ := newCodexBundleTestState(t)
	err := printReviewGap(cfg, st, "missing-task", &bytes.Buffer{})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("missing task error = %#v", err)
	}
}
