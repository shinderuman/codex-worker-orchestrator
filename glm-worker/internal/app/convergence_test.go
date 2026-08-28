package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func convergenceBaseTime() time.Time {
	return time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
}

func matchedTrue() *bool {
	value := true
	return &value
}

func appendConvergenceRound(t *testing.T, st *state.StateStore, record state.RoundRecord) {
	t.Helper()
	if err := st.AppendRoundRecord(record); err != nil {
		t.Fatal(err)
	}
}

func executeConvergenceOutput(t *testing.T, st *state.StateStore, taskID string) convergenceOutput {
	t.Helper()
	var out bytes.Buffer
	if err := printConvergence(st, taskID, &out); err != nil {
		t.Fatal(err)
	}
	var output convergenceOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("convergence出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	return output
}

func convergenceSummaryOf(t *testing.T, output convergenceOutput, class string) convergenceClassSummary {
	t.Helper()
	for _, summary := range output.Summary.ByClass {
		if summary.Class == class {
			return summary
		}
	}
	t.Fatalf("summary.by_classに%qがありません: %#v", class, output.Summary.ByClass)
	return convergenceClassSummary{}
}

func TestConvergenceRendersRoundsCostsAndSummary(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()
	snapshot := state.SnapshotDigest{Head: "head1", IndexDigest: "index1", WorktreeDigest: "worktree1"}
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base,
		Snapshot: snapshot, Paths: []state.RoundPathState{},
	})
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: snapshot, Paths: []state.RoundPathState{},
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.WorkerRole, Phase: "worker-new",
		StartedAt: base, CompletedAt: base.Add(5 * time.Second),
		TreeUsage:      state.TokenUsage{InputTokens: 100, OutputTokens: 40},
		WallDurationMS: 5000, TopLevelTurns: 3,
	})

	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.WorkerRole, Phase: "worker-new-packet-compact",
		StartedAt: base.Add(5 * time.Second), CompletedAt: base.Add(6 * time.Second),
		TreeUsage:      state.TokenUsage{InputTokens: 20, OutputTokens: 5},
		WallDurationMS: 1000, TopLevelTurns: 1,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.ReviewerRole, Phase: "reviewer-1",
		StartedAt: base.Add(20 * time.Second), CompletedAt: base.Add(25 * time.Second),
		TreeUsage:      state.TokenUsage{InputTokens: 200, CacheReadInputTokens: 50, OutputTokens: 10},
		WallDurationMS: 5000, TopLevelTurns: 2, PacketStatus: "PASS",
		EffectiveRisk: "LOW", ReviewerReportedRisk: "LOW",
		Snapshot: &state.SnapshotDiagnostic{Stage: "review-end", Matched: matchedTrue()},
	})
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Timestamp: base.Add(time.Second), Kind: "assistant", Blocks: []state.TaskBlockSummary{
			{Type: "tool_use", Name: "Bash", ToolID: "t1"},
			{Type: "tool_use", Name: "Read", ToolID: "t2"},
		}},
	)

	output := executeConvergenceOutput(t, st, "")
	if output.TaskID != taskID {
		t.Fatalf("task_id = %q", output.TaskID)
	}
	if output.RoundsLog.Status != "ok" || output.RoundsLog.Path == nil || *output.RoundsLog.Path != st.RoundLogPath(taskID) {
		t.Fatalf("rounds_log = %#v", output.RoundsLog)
	}
	if output.Telemetry != "ok" || output.EventLog != "ok" {
		t.Fatalf("telemetry/event_log = %q/%q", output.Telemetry, output.EventLog)
	}
	if output.Baseline == nil || output.Baseline.CapturedAt == nil || !output.Baseline.CapturedAt.Equal(base) ||
		output.Baseline.Paths != 0 || !output.Baseline.SnapshotKnown || output.Baseline.CaptureError != "" {
		t.Fatalf("baseline = %#v", output.Baseline)
	}
	if output.Baseline.Snapshot.Head != "head1" || output.Baseline.Snapshot.IndexDigest != "index1" || output.Baseline.Snapshot.WorktreeDigest != "worktree1" {
		t.Fatalf("baseline snapshot = %#v", output.Baseline.Snapshot)
	}
	if len(output.Rounds) != 1 {
		t.Fatalf("rounds = %#v", output.Rounds)
	}
	round := output.Rounds[0]
	if round.Number != 1 || round.Seq != 2 || round.ReviewNumber != 1 || round.AutoFixes != 0 || round.WorkerPhase != "worker-new" {
		t.Fatalf("round #1 = %#v", round)
	}
	if round.Delta.Class != "verification-only" || round.Delta.ChangedPaths != 0 || round.Delta.NonSemanticPaths != 0 || round.Delta.DocPaths != 0 {
		t.Fatalf("round #1 delta = %#v", round.Delta)
	}
	if round.Snapshot.Head != "head1" || round.Snapshot.IndexDigest != "index1" || round.Snapshot.WorktreeDigest != "worktree1" {
		t.Fatalf("round #1 snapshot = %#v", round.Snapshot)
	}
	if round.Review.Calls != 1 || round.Review.Outcome == nil || *round.Review.Outcome != "PASS" ||
		round.Review.Risk == nil || *round.Review.Risk != "LOW" || round.Review.ReportedRisk == nil || *round.Review.ReportedRisk != "LOW" ||
		round.Review.RiskFloorReemit || round.Review.Unresolved || round.Review.Snapshot != "matched" {
		t.Fatalf("round #1 review = %#v", round.Review)
	}
	if round.ReviewerCost == nil || round.ReviewerCost.Calls != 1 || round.ReviewerCost.InputTokens != 250 || round.ReviewerCost.OutputTokens != 10 ||
		round.ReviewerCost.Turns != 2 || round.ReviewerCost.DurationMS != 5000 {
		t.Fatalf("round #1 reviewer_cost = %#v", round.ReviewerCost)
	}
	if round.WorkerCost == nil || round.WorkerCost.Calls != 1 || round.WorkerCost.InputTokens != 100 || round.WorkerCost.OutputTokens != 40 ||
		round.WorkerCost.Turns != 3 || round.WorkerCost.DurationMS != 5000 {
		t.Fatalf("round #1 worker_cost = %#v", round.WorkerCost)
	}
	summary := convergenceSummaryOf(t, output, "verification-only")
	if summary.Rounds != 1 || summary.ReviewerCalls != 1 || summary.ReviewerInputTokens != 250 || summary.ReviewerOutputTokens != 10 || summary.ReviewerDurationMS != 5000 {
		t.Fatalf("summary = %#v", summary)
	}
	if output.Summary.UnresolvedIssueRounds != 0 || output.Summary.HighRounds != 0 {
		t.Fatalf("summary counters = %#v", output.Summary)
	}
}

func TestConvergenceRendersDocChangeRound(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base,
		Snapshot: state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"},
		Paths: []state.RoundPathState{
			{Path: "AGENTS.md", Class: state.RoundPathClassDoc, FullDigest: "ad1", SemanticDigest: "ad1"},
		},
	})
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w2"},
		Paths: []state.RoundPathState{
			{Path: "AGENTS.md", Class: state.RoundPathClassDoc, FullDigest: "ad2", SemanticDigest: "ad2"},
		},
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.ReviewerRole, Phase: "reviewer-1",
		StartedAt: base.Add(20 * time.Second), CompletedAt: base.Add(25 * time.Second),
		TreeUsage:      state.TokenUsage{InputTokens: 200, OutputTokens: 10},
		WallDurationMS: 5000, TopLevelTurns: 2, PacketStatus: "PASS",
		EffectiveRisk: "LOW", ReviewerReportedRisk: "LOW",
	})

	output := executeConvergenceOutput(t, st, "")
	if len(output.Rounds) != 1 {
		t.Fatalf("rounds = %#v", output.Rounds)
	}
	if output.Rounds[0].Delta.Class != "doc-change" || output.Rounds[0].Delta.ChangedPaths != 1 ||
		output.Rounds[0].Delta.NonSemanticPaths != 0 || output.Rounds[0].Delta.DocPaths != 1 {
		t.Fatalf("round #1 delta = %#v", output.Rounds[0].Delta)
	}
	summary := convergenceSummaryOf(t, output, "doc-change")
	if summary.Rounds != 1 || summary.ReviewerCalls != 1 || summary.ReviewerInputTokens != 200 || summary.ReviewerOutputTokens != 10 || summary.ReviewerDurationMS != 5000 {
		t.Fatalf("summary = %#v", summary)
	}
	for _, classSummary := range output.Summary.ByClass {
		if classSummary.Class == "comment-format-only" {
			t.Fatalf("doc変更roundがcomment/format-onlyへ集計されています: %#v", classSummary)
		}
	}
}

func TestConvergenceMutatingToolUseStaysSameSnapshot(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()
	snapshot := state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"}
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base, Snapshot: snapshot,
	})
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: snapshot,
	})
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Timestamp: base.Add(time.Second), Kind: "assistant", Blocks: []state.TaskBlockSummary{
			{Type: "tool_use", Name: "Edit", ToolID: "t1"},
		}},
	)

	output := executeConvergenceOutput(t, st, "")
	if len(output.Rounds) != 1 || output.Rounds[0].Delta.Class != "same-snapshot" {
		t.Fatalf("rounds = %#v", output.Rounds)
	}
	for _, classSummary := range output.Summary.ByClass {
		if classSummary.Class == "verification-only" {
			t.Fatalf("file変更tool観測roundがverification-onlyへ細分化されています: %#v", classSummary)
		}
	}
}

func TestReviewerCallsInBucketRecognizesCurrentReviewerPhaseGrammar(t *testing.T) {
	entries := []state.ModelCallLog{
		{Role: state.ReviewerRole, Phase: "reviewer-2"},
		{Role: state.ReviewerRole, Phase: "reviewer-2-result-correct"},
		{Role: state.ReviewerRole, Phase: "reviewer-2-risk-floor"},
		{Role: state.ReviewerRole, Phase: "reviewer-2-risk-floor-result-correct"},
		{Role: state.ReviewerRole, Phase: "reviewer-2-high-floor"},
		{Role: state.ReviewerRole, Phase: "reviewer-2-high-floor-result-correct"},
	}
	mismatch := false
	got := reviewerCallsInBucket(entries, 2, &mismatch)
	if mismatch || len(got) != len(entries) {
		t.Fatalf("valid reviewer phases were not attributed: mismatch=%v calls=%#v", mismatch, got)
	}

	mismatch = false
	got = reviewerCallsInBucket([]state.ModelCallLog{{Role: state.ReviewerRole, Phase: "reviewer-2-future-floor"}}, 2, &mismatch)
	if !mismatch || len(got) != 0 {
		t.Fatalf("unknown reviewer phase must remain mismatch: mismatch=%v calls=%#v", mismatch, got)
	}
}

func TestConvergenceHighFloorReviewerDoesNotCreateSubsequentGap(t *testing.T) {
	base := convergenceBaseTime()
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
	rounds, _ := buildConvergenceRounds(records, logs)
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
	riskRound := convergenceRound{reviewer: []state.ModelCallLog{{
		Role: state.ReviewerRole, Phase: "reviewer-1-risk-floor-result-correct",
	}}}
	if got := convergenceReviewOutDetail(riskRound); !got.RiskFloorReemit {
		t.Fatalf("risk-floor result correction lost risk-floor identity: %#v", got)
	}
	highRound := convergenceRound{reviewer: []state.ModelCallLog{{
		Role: state.ReviewerRole, Phase: "reviewer-1-high-floor-result-correct",
	}}}
	if got := convergenceReviewOutDetail(highRound); got.RiskFloorReemit {
		t.Fatalf("high-floor result correction was conflated with risk-floor: %#v", got)
	}
}
func TestConvergenceGapAndMismatchFallToUnknown(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()
	snapshot := state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"}
	changed := state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w2"}
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base, Snapshot: snapshot,
	})

	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: snapshot, Paths: []state.RoundPathState{},
	})

	jumped := state.RoundRecord{
		Version: 1, TaskID: taskID, Seq: 5, ReviewNumber: 2, WorkerPhase: "worker-auto-fix-1",
		CapturedAt: base.Add(30 * time.Second), Snapshot: changed,
		Paths: []state.RoundPathState{
			{Path: "main.go", Class: state.RoundPathClassCode, FullDigest: "f2", SemanticDigest: "s1"},
		},
	}
	jumpedData, err := json.Marshal(jumped)
	if err != nil {
		t.Fatal(err)
	}
	roundFile, err := os.OpenFile(st.RoundLogPath(taskID), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roundFile.Write(append(jumpedData, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := roundFile.Close(); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.ReviewerRole, Phase: "reviewer-2",
		StartedAt: base.Add(20 * time.Second), CompletedAt: base.Add(21 * time.Second),
		PacketStatus: "PASS", WallDurationMS: 1000,
	})

	output := executeConvergenceOutput(t, st, "")
	if len(output.Rounds) != 2 {
		t.Fatalf("rounds = %#v", output.Rounds)
	}
	first := output.Rounds[0]
	if first.Delta.Class != "same-snapshot" || first.Delta.ChangedPaths != 0 || first.Delta.NonSemanticPaths != 0 || first.Delta.DocPaths != 0 || !first.Delta.MismatchedReviewer {
		t.Fatalf("round 1 delta = %#v", first.Delta)
	}
	second := output.Rounds[1]
	if second.Number != 2 || second.Seq != 5 || second.Delta.Class != "unknown" || !second.Delta.Gap {
		t.Fatalf("round 2 = %#v", second)
	}
	for _, classSummary := range output.Summary.ByClass {
		if classSummary.Class == "comment-format-only" {
			t.Fatalf("欠落疑いroundの分類がunknownへ倒されていません: %#v", classSummary)
		}
	}
	summary := convergenceSummaryOf(t, output, "unknown")
	if summary.Rounds != 1 {
		t.Fatalf("unknown summary = %#v", summary)
	}
}

func TestConvergenceUnresolvedAndHighCounters(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()

	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: state.SnapshotDigest{Head: "h2", IndexDigest: "i2", WorktreeDigest: "w2"},
		Paths: []state.RoundPathState{
			{Path: "main.go", Class: state.RoundPathClassCode, FullDigest: "f1", SemanticDigest: "s1"},
		},
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, Role: state.ReviewerRole, Phase: "reviewer-1",
		StartedAt: base.Add(20 * time.Second), PacketStatus: "FIX_REQUIRED",
		EffectiveRisk: "HIGH", ReviewerReportedRisk: "LOW", WallDurationMS: 1000,
	})

	output := executeConvergenceOutput(t, st, "")
	if len(output.Rounds) != 1 {
		t.Fatalf("rounds = %#v", output.Rounds)
	}
	round := output.Rounds[0]
	if round.Delta.Class != "initial" || round.Delta.ChangedPaths != 0 || round.Delta.NonSemanticPaths != 0 || round.Delta.DocPaths != 0 {
		t.Fatalf("round #1 delta = %#v", round.Delta)
	}
	if round.Review.Calls != 1 || round.Review.Outcome == nil || *round.Review.Outcome != "FIX_REQUIRED" ||
		round.Review.Risk == nil || *round.Review.Risk != "HIGH" || round.Review.ReportedRisk == nil || *round.Review.ReportedRisk != "LOW" ||
		round.Review.RiskFloorReemit || !round.Review.Unresolved || round.Review.Snapshot != "unknown" {
		t.Fatalf("round #1 review = %#v", round.Review)
	}
	summary := convergenceSummaryOf(t, output, "initial")
	if summary.Rounds != 1 || summary.ReviewerCalls != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if output.Summary.UnresolvedIssueRounds != 1 {
		t.Fatalf("unresolved_issue_rounds = %d", output.Summary.UnresolvedIssueRounds)
	}
	if output.Summary.HighRounds != 1 {
		t.Fatalf("high_rounds = %d", output.Summary.HighRounds)
	}
}

func TestConvergenceSkipsCorruptRoundLines(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := convergenceBaseTime()
	snapshot := state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"}
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base, Snapshot: snapshot,
	})
	appendConvergenceRound(t, st, state.RoundRecord{
		TaskID: taskID, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(10 * time.Second),
		Snapshot: snapshot,
	})
	roundFile, err := os.OpenFile(st.RoundLogPath(taskID), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roundFile.WriteString("{\"version\":1,\"kind\":\"brokencorrupt\n"); err != nil {
		t.Fatal(err)
	}
	if err := roundFile.Close(); err != nil {
		t.Fatal(err)
	}

	output := executeConvergenceOutput(t, st, "")
	if output.SkippedRounds != 1 {
		t.Fatalf("skipped_rounds = %d", output.SkippedRounds)
	}
	if len(output.Rounds) != 1 || output.Rounds[0].Number != 1 || output.Rounds[0].Seq != 2 || output.Rounds[0].ReviewNumber != 1 ||
		output.Rounds[0].AutoFixes != 0 || output.Rounds[0].WorkerPhase != "worker-new" {
		t.Fatalf("破損行以前のrecord表示がありません: %#v", output.Rounds)
	}
}

func TestConvergenceCurrentTaskWithoutRecords(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	output := executeConvergenceOutput(t, st, "")
	if output.RoundsLog.Status != "none" || output.RoundsLog.Path != nil {
		t.Fatalf("rounds_log = %#v", output.RoundsLog)
	}
	if output.Rounds != nil {
		t.Fatalf("round logがないのにround表示 = %#v", output.Rounds)
	}
}

func TestConvergenceExplicitTaskMissingRoundLog(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	if err := printConvergence(st, "12345678-1234-4234-8123-123456789abc", out); err == nil {
		t.Fatalf("不在task IDがerrorになりません: %s", out.String())
	}
}

func TestConvergenceRejectsTaskIDOutsideGeneratedForm(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeTimelineSentinel(t, cfg)

	for _, taskID := range []string{"../../evil", "/etc/hostname", "12345678-1234-1234-8123-123456789abc", "none"} {
		out := &bytes.Buffer{}
		if err := printConvergence(st, taskID, out); err == nil {
			t.Fatalf("不正task ID %qがerrorになりません: %s", taskID, out.String())
		}
		if body := out.String(); body != "" {
			t.Fatalf("不正task ID %qが出力しました: %s", taskID, body)
		}
	}
}

func TestParseCommandConvergence(t *testing.T) {
	cmd, err := ParseCommand([]string{"--convergence", "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeConvergence || cmd.Payload != "task-1" {
		t.Fatalf("command = %+v", cmd)
	}
	cmd, err = ParseCommand([]string{"--convergence"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeConvergence || cmd.Payload != "" {
		t.Fatalf("command = %+v", cmd)
	}
	if _, err := ParseCommand([]string{"--convergence", "task-1", "extra"}); err == nil {
		t.Fatal("余分な引数が受け入れられています")
	}
}

func TestExecuteConvergenceDoesNotCreateState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "convergencehash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--convergence"})
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output convergenceOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("convergence出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.TaskID != "" || output.RoundsLog.Status != "none" {
		t.Fatalf("convergence出力 = %#v", output)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--convergenceがstate dirを作成しました: %v", entries)
	}
}
