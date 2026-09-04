package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentUsageReportIntervals(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), parentUsagePhaseLines(t, task.start, task.completeAt))
	guardedBefore := parentUsageStateSnapshot(t, task.st)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeParentUsage}, task.cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var report parentUsageReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	if report.Version != parentUsageReportVersion || report.TaskID != task.taskID {
		t.Fatalf("report header = %#v", report)
	}
	parent := report.ParentSession
	if parent.Status != codexStatusIncluded || parent.ThreadID != codexTestParentThreadID ||
		parent.AssociationBasis != codexAssociationBasis || parent.RolloutSource != analysisRolloutRel() {
		t.Fatalf("parent session = %#v", parent)
	}

	execution := report.Intervals.TaskExecution
	if execution.Status != analysisStatusAvailable || execution.End == nil ||
		execution.EndBasis != analysisExecutionEndBasisLifecycleComplete {
		t.Fatalf("execution interval = %#v", execution)
	}
	executionTokens := execution.Tokens
	if executionTokens.Status != analysisStatusAvailable ||
		executionTokens.InputTokens != 1000 || executionTokens.CachedInputTokens != 500 ||
		executionTokens.OutputTokens != 160 || executionTokens.ReasoningTokens != 80 ||
		executionTokens.TotalTokens != 1500 {
		t.Fatalf("execution tokens = %#v", executionTokens)
	}
	if executionTokens.BaselineSource != parentUsageSourceLocator(analysisRolloutRel(), 2) ||
		executionTokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 8) ||
		executionTokens.BaselineAt != task.start.Add(-time.Minute).Format(time.RFC3339Nano) ||
		executionTokens.EndAt != task.completeAt.Add(-time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("execution token anchors = %#v", executionTokens)
	}
	if len(executionTokens.UnknownFields) != 0 {
		t.Fatalf("execution unknown fields = %#v", executionTokens.UnknownFields)
	}
	executionActivity := execution.Activity
	if executionActivity.Status != analysisStatusCounted ||
		executionActivity.ModelTurns != 1 || executionActivity.ToolCalls != 1 ||
		executionActivity.ToolResults != 1 || executionActivity.Compactions != 1 ||
		executionActivity.ToolOutputBytes != 10 ||
		executionActivity.Source != analysisRolloutRel() {
		t.Fatalf("execution activity = %#v", executionActivity)
	}

	finalization := report.Intervals.ParentFinalization
	if finalization.Status != analysisStatusAvailable || finalization.Start == nil || finalization.End == nil ||
		*finalization.Start != task.completeAt.Format(time.RFC3339Nano) ||
		*finalization.End != task.completeAt.Add(2*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("finalization interval = %#v", finalization)
	}
	finalizationTokens := finalization.Tokens
	if finalizationTokens.Status != analysisStatusAvailable ||
		finalizationTokens.InputTokens != 600 || finalizationTokens.CachedInputTokens != 300 ||
		finalizationTokens.OutputTokens != 80 || finalizationTokens.ReasoningTokens != 20 ||
		finalizationTokens.TotalTokens != 800 {
		t.Fatalf("finalization tokens = %#v", finalizationTokens)
	}
	if finalizationTokens.BaselineSource != parentUsageSourceLocator(analysisRolloutRel(), 8) ||
		finalizationTokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 9) {
		t.Fatalf("finalization token anchors = %#v", finalizationTokens)
	}
	finalizationActivity := finalization.Activity
	if finalizationActivity.Status != analysisStatusCounted ||
		finalizationActivity.ModelTurns != 0 || finalizationActivity.ToolCalls != 1 ||
		finalizationActivity.ToolResults != 1 || finalizationActivity.Compactions != 0 ||
		finalizationActivity.ToolOutputBytes != 10 {
		t.Fatalf("finalization activity = %#v", finalizationActivity)
	}

	if exportsDir := filepath.Join(filepath.Dir(task.cfg.StateBase), "exports", task.cfg.RepoHash); dirExists(exportsDir) {
		t.Fatalf("parent usage created a bundle export: %s", exportsDir)
	}
	if !bytes.Equal(guardedBefore, parentUsageStateSnapshot(t, task.st)) {
		t.Fatal("parent usage reportが原本stateを変更しました")
	}
}

func TestParentUsageReportMatchesBundleAnalysis(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), parentUsagePhaseLines(t, task.start, task.completeAt))

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeParentUsage}, task.cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var report parentUsageReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	index := runAnalysisBundle(t, task.cfg, "")

	if report.Intervals.TaskExecution.Tokens.InputTokens != index.TokenDelta.InputTokens ||
		report.Intervals.TaskExecution.Tokens.CachedInputTokens != index.TokenDelta.CachedInputTokens ||
		report.Intervals.TaskExecution.Tokens.Status != index.TokenDelta.Status {
		t.Fatalf("execution tokens = %#v want bundle %#v", report.Intervals.TaskExecution.Tokens, index.TokenDelta)
	}
	if report.Intervals.ParentFinalization.Tokens.InputTokens != index.Finalization.InputTokens ||
		report.Intervals.ParentFinalization.Tokens.CachedInputTokens != index.Finalization.CachedInputTokens ||
		report.Intervals.ParentFinalization.Tokens.Status != index.Finalization.Status {
		t.Fatalf("finalization tokens = %#v want bundle %#v", report.Intervals.ParentFinalization.Tokens, index.Finalization)
	}
	if report.ParentSession.Status != index.ParentSession.Status ||
		report.ParentSession.ThreadID != index.ParentSession.ThreadID {
		t.Fatalf("parent session = %#v want bundle %#v", report.ParentSession, index.ParentSession)
	}
}

func TestParentUsageSharedBoundaryPartition(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	boundary := task.completeAt
	lines := []string{
		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, boundary.Add(-6*time.Minute), 1500, 700, 300, 200, 2300),
		analysisTurnLine(t, boundary.Add(-5*time.Minute), codexRolloutTaskStartedType, analysisStraddleTurnID),
		parentUsageToolCallLine(t, boundary.Add(-30*time.Second), "parent-usage-exec"),
		parentUsageToolCallLine(t, boundary, "parent-usage-boundary"),
		parentUsageToolOutputLine(t, boundary, "parent-usage-boundary", "0123456789"),
		parentUsageCompactedLine(t, boundary),
		parentUsageTokenCountLine(t, boundary, 2000, 1000, 400, 240, 3000),
		analysisTurnLine(t, boundary, codexRolloutTaskStartedType, analysisBoundaryStartTurnID),
		analysisTurnLine(t, boundary, codexRolloutTaskStartedType, analysisBoundaryTurnID),
		analysisTurnLine(t, boundary, codexRolloutTaskCompleteType, analysisBoundaryTurnID),
		analysisTurnLine(t, boundary.Add(time.Minute), codexRolloutTaskCompleteType, analysisBoundaryStartTurnID),
		analysisTurnLine(t, boundary.Add(90*time.Second), codexRolloutTaskStartedType, analysisPostBoundaryTurnID),
		parentUsageToolCallLine(t, boundary.Add(90*time.Second), "parent-usage-final"),
		analysisTurnLine(t, boundary.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisStraddleTurnID),
		parentUsageTokenCountLine(t, boundary.Add(2*time.Minute), 2600, 1300, 480, 260, 3800),
		parentUsageToolOutputLine(t, boundary.Add(2*time.Minute), "parent-usage-final", "abcd"),
		analysisTurnLine(t, boundary.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisPostBoundaryTurnID),
		analysisTurnLine(t, boundary.Add(3*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	execution := report.Intervals.TaskExecution
	if execution.Status != analysisStatusAvailable || execution.End == nil ||
		*execution.End != boundary.Format(time.RFC3339Nano) {
		t.Fatalf("execution interval = %#v", execution)
	}
	executionActivity := execution.Activity
	if executionActivity.Status != analysisStatusCounted ||
		executionActivity.ModelTurns != 4 || executionActivity.ToolCalls != 2 ||
		executionActivity.ToolResults != 1 || executionActivity.Compactions != 1 ||
		executionActivity.ToolOutputBytes != 10 {
		t.Fatalf("execution activity = %#v", executionActivity)
	}
	executionTokens := execution.Tokens
	if executionTokens.Status != analysisStatusAvailable ||
		executionTokens.InputTokens != 1000 || executionTokens.CachedInputTokens != 500 ||
		executionTokens.OutputTokens != 160 || executionTokens.ReasoningTokens != 80 ||
		executionTokens.TotalTokens != 1500 {
		t.Fatalf("execution tokens = %#v", executionTokens)
	}
	if executionTokens.EndAt != boundary.Format(time.RFC3339Nano) ||
		executionTokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 10) {
		t.Fatalf("execution token anchors = %#v", executionTokens)
	}

	finalization := report.Intervals.ParentFinalization
	if finalization.Status != analysisStatusAvailable || finalization.Start == nil || finalization.End == nil ||
		*finalization.Start != boundary.Format(time.RFC3339Nano) ||
		*finalization.End != boundary.Add(3*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("finalization interval = %#v", finalization)
	}
	finalizationActivity := finalization.Activity
	if finalizationActivity.Status != analysisStatusCounted ||
		finalizationActivity.ModelTurns != 1 || finalizationActivity.ToolCalls != 1 ||
		finalizationActivity.ToolResults != 1 || finalizationActivity.Compactions != 0 ||
		finalizationActivity.ToolOutputBytes != 4 {
		t.Fatalf("finalization activity = %#v", finalizationActivity)
	}
	if executionActivity.ModelTurns+finalizationActivity.ModelTurns != 5 {
		t.Fatalf("model turn partition = %#v / %#v", executionActivity, finalizationActivity)
	}
	finalizationTokens := finalization.Tokens
	if finalizationTokens.Status != analysisStatusAvailable ||
		finalizationTokens.InputTokens != 600 || finalizationTokens.CachedInputTokens != 300 ||
		finalizationTokens.OutputTokens != 80 || finalizationTokens.ReasoningTokens != 20 ||
		finalizationTokens.TotalTokens != 800 {
		t.Fatalf("finalization tokens = %#v", finalizationTokens)
	}
	if finalizationTokens.BaselineAt != boundary.Format(time.RFC3339Nano) ||
		finalizationTokens.BaselineSource != executionTokens.EndSource ||
		finalizationTokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 18) {
		t.Fatalf("finalization token anchors = %#v", finalizationTokens)
	}
}

func TestParentUsageUnreadableRolloutDistinctFromAbsentEvidence(t *testing.T) {
	t.Run("readable-rollout-without-evidence", func(t *testing.T) {
		task := newAnalysisTerminalTask(t)
		writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
			task.start.Add(-3*time.Hour), nil)

		report := runParentUsageReport(t, task.cfg)
		execution := report.Intervals.TaskExecution
		if execution.Tokens.Status != analysisStatusMissing ||
			execution.Tokens.Reason != parentUsageReasonBaselineAnchor {
			t.Fatalf("execution tokens = %#v", execution.Tokens)
		}
		if execution.Activity.Status != analysisStatusNoObservation {
			t.Fatalf("execution activity = %#v", execution.Activity)
		}
		finalization := report.Intervals.ParentFinalization
		if finalization.Tokens.Status != analysisStatusUnknown ||
			finalization.Tokens.Reason != parentUsageReasonFinalizationInterval ||
			finalization.Activity.Status != analysisStatusUnknown ||
			finalization.Activity.Reason != parentUsageReasonFinalizationInterval {
			t.Fatalf("finalization interval = %#v", finalization)
		}
	})

	t.Run("unreadable-rollout", func(t *testing.T) {
		association := codexAssociation{
			ParentStatus: codexStatusIncluded,
			ParentPath:   filepath.Join(t.TempDir(), "unreadable-rollout"),
			ParentSource: "sessions/2026/09/04/rollout-unreadable.jsonl",
		}
		if err := os.Mkdir(association.ParentPath, 0o700); err != nil {
			t.Fatal(err)
		}
		start := time.Now().UTC().Add(-time.Hour)
		end := time.Now().UTC()
		scan, scanErr := parentUsageRolloutScan(association, start, end)
		if scanErr == nil {
			t.Fatal("rollout scan errorが握り潰されました")
		}
		missing, missingErr := parentUsageRolloutScan(codexAssociation{
			ParentStatus: codexStatusIncluded,
			ParentPath:   filepath.Join(t.TempDir(), "absent-rollout.jsonl"),
		}, start, end)
		if missingErr == nil || missing.hasWindow {
			t.Fatalf("absent rollout scan = %#v, %v", missing, missingErr)
		}

		executionBoundary := analysisExecutionBoundary{
			status:   analysisStatusAvailable,
			end:      start.Add(30 * time.Minute),
			endBasis: analysisExecutionEndBasisLifecycleComplete,
		}
		execution := parentUsageExecutionInterval(association, scan, scanErr, start, executionBoundary, end)
		if execution.Status != analysisStatusAvailable || execution.End == nil {
			t.Fatalf("execution interval = %#v", execution)
		}
		if execution.Tokens.Status != analysisStatusUnreadable ||
			execution.Tokens.Reason != analysisReasonRolloutScanFailed ||
			execution.Tokens.InputTokens != 0 || execution.Tokens.TotalTokens != 0 {
			t.Fatalf("execution tokens = %#v", execution.Tokens)
		}
		if execution.Activity.Status != analysisStatusUnreadable ||
			execution.Activity.Reason != analysisReasonRolloutScanFailed ||
			execution.Activity.Source != association.ParentSource ||
			execution.Activity.ToolCalls != 0 || execution.Activity.ModelTurns != 0 {
			t.Fatalf("execution activity = %#v", execution.Activity)
		}

		turn := &analysisRolloutTurn{
			TurnID:      analysisOwningTurnID,
			StartedAt:   start.Add(-time.Minute),
			HasStart:    true,
			CompletedAt: end,
			HasComplete: true,
		}
		ownership := analysisTaskOwnership{
			status:  analysisStatusAvailable,
			initial: turn,
			final:   turn,
			owned:   map[string]struct{}{analysisOwningTurnID: {}},
		}
		window := bundleAnalysisInterval{
			Status: analysisStatusAvailable,
			Start:  analysisTimestamp(executionBoundary.end),
			End:    analysisTimestamp(end),
		}
		finalization := parentUsageFinalizationInterval(association, scan, scanErr, executionBoundary, ownership, window)
		if finalization.Status != analysisStatusAvailable ||
			finalization.Tokens.Status != analysisStatusUnreadable ||
			finalization.Tokens.Reason != analysisReasonRolloutScanFailed ||
			finalization.Activity.Status != analysisStatusUnreadable ||
			finalization.Activity.Reason != analysisReasonRolloutScanFailed ||
			finalization.Activity.Source != association.ParentSource {
			t.Fatalf("finalization interval = %#v", finalization)
		}
	})
}

func TestParentUsageCounterResetWithLocators(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	lines := []string{
		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 1200, 600, 300, 200, 900),
		analysisTurnLine(t, task.completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	tokens := report.Intervals.TaskExecution.Tokens
	if tokens.Status != analysisStatusCounterReset || tokens.InputTokens != 0 || tokens.TotalTokens != 0 {
		t.Fatalf("execution tokens = %#v", tokens)
	}
	if tokens.BaselineSource != parentUsageSourceLocator(analysisRolloutRel(), 2) ||
		tokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 4) {
		t.Fatalf("counter reset locators = %#v", tokens)
	}
	if report.Intervals.TaskExecution.Activity.Status != analysisStatusCounted {
		t.Fatalf("execution activity = %#v", report.Intervals.TaskExecution.Activity)
	}
}

func TestParentUsageMissingTokenFields(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	lines := []string{
		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisTokenCountLine(t, task.completeAt.Add(-time.Second), 2000, 1000),
		analysisTurnLine(t, task.completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	tokens := report.Intervals.TaskExecution.Tokens
	if tokens.Status != analysisStatusAvailable || tokens.InputTokens != 1000 || tokens.CachedInputTokens != 500 {
		t.Fatalf("execution tokens = %#v", tokens)
	}
	fields := map[string]parentUsageUnknownField{}
	for _, unknown := range tokens.UnknownFields {
		fields[unknown.Field] = unknown
	}
	for _, field := range []string{parentUsageFieldOutput, parentUsageFieldReasoning, parentUsageFieldTotal} {
		unknown, present := fields[field]
		if !present {
			t.Fatalf("field %s is not reported unknown: %#v", field, tokens.UnknownFields)
		}
		if unknown.Reason != parentUsageReasonMissingInEnd ||
			unknown.Source != parentUsageSourceLocator(analysisRolloutRel(), 4) {
			t.Fatalf("unknown field %s = %#v", field, unknown)
		}
	}
	if len(tokens.UnknownFields) != 3 {
		t.Fatalf("unknown fields = %#v", tokens.UnknownFields)
	}
}

func TestParentUsageIdentityDegradations(t *testing.T) {
	t.Run("missing-identity", func(t *testing.T) {
		cfg, st, codexHome := newCodexBundleTestState(t)
		taskID, err := st.StartNewTask()
		if err != nil {
			t.Fatal(err)
		}
		stats, err := st.CurrentTaskStats()
		if err != nil {
			t.Fatal(err)
		}
		writeAnalysisRollout(t, codexHome, analysisRolloutRel(), codexTestParentThreadID,
			stats.StartedAt.UTC().Add(-3*time.Hour), nil)

		var stdout bytes.Buffer
		if err := Execute(Command{Mode: ModeParentUsage}, cfg, nil, &stdout, nil); err != nil {
			t.Fatal(err)
		}
		var report parentUsageReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.TaskID != taskID || report.ParentSession.Status != codexStatusMissing {
			t.Fatalf("report = %#v", report)
		}
		for _, interval := range []parentUsageInterval{report.Intervals.TaskExecution, report.Intervals.ParentFinalization} {
			if interval.Tokens.Status != codexStatusMissing || interval.Activity.Status != codexStatusMissing {
				t.Fatalf("degraded interval = %#v", interval)
			}
			if interval.Tokens.InputTokens != 0 || interval.Activity.ToolCalls != 0 {
				t.Fatalf("degraded interval carries values: %#v", interval)
			}
		}
	})

	t.Run("ambiguous-rollout", func(t *testing.T) {
		cfg, st, codexHome := newCodexBundleTestState(t)
		_, err := st.StartNewTask()
		if err != nil {
			t.Fatal(err)
		}
		stats, err := st.CurrentTaskStats()
		if err != nil {
			t.Fatal(err)
		}
		start := stats.StartedAt.UTC()
		if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{
			analysisRolloutRel(),
			"archived_sessions/rollout-archived-" + codexTestParentThreadID + ".jsonl",
		} {
			writeAnalysisRollout(t, codexHome, rel, codexTestParentThreadID,
				start.Add(-3*time.Hour), nil)
		}

		report := runParentUsageReport(t, cfg)
		if report.ParentSession.Status != codexStatusAmbiguous ||
			!strings.Contains(report.ParentSession.Detail, "2 rollouts share the stored parent thread ID") {
			t.Fatalf("parent session = %#v", report.ParentSession)
		}
		if report.Intervals.TaskExecution.Tokens.Status != codexStatusAmbiguous ||
			report.Intervals.TaskExecution.Activity.Status != codexStatusAmbiguous {
			t.Fatalf("execution interval = %#v", report.Intervals.TaskExecution)
		}
	})

	t.Run("unknown-task", func(t *testing.T) {
		cfg, _, _ := newCodexBundleTestState(t)
		var stdout bytes.Buffer
		err := Execute(Command{Mode: ModeParentUsage, Payload: "task-missing"}, cfg, nil, &stdout, nil)
		if err == nil {
			t.Fatal("unknown taskで成功しました")
		}
		var notFoundError *NotFoundError
		if !errors.As(err, &notFoundError) {
			t.Fatalf("error = %#v", err)
		}
		if stdout.Len() != 0 {
			t.Fatalf("error時のstdout = %s", stdout.String())
		}
	})
}

func TestParentUsageOpenTaskReportsProgress(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	inWindow := time.Now().UTC()
	if inWindow.Before(start) {
		inWindow = start
	}
	lines := []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageToolCallLine(t, inWindow, "parent-usage-open"),
		parentUsageTokenCountLine(t, inWindow, 1600, 800, 320, 220, 2300),
	}
	writeAnalysisRollout(t, codexHome, analysisRolloutRel(), codexTestParentThreadID,
		start.Add(-3*time.Hour), lines)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeParentUsage}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var report parentUsageReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.TaskID != taskID || report.TaskStatus != string(st.TaskStatus()) {
		t.Fatalf("report = %#v", report)
	}
	execution := report.Intervals.TaskExecution
	if execution.Status != analysisStatusOpen || execution.End != nil {
		t.Fatalf("execution interval = %#v", execution)
	}
	if execution.Tokens.Status != analysisStatusOpen || execution.Tokens.InputTokens != 600 ||
		execution.Tokens.TotalTokens != 800 {
		t.Fatalf("execution tokens = %#v", execution.Tokens)
	}
	if execution.Activity.Status != analysisStatusOpen || execution.Activity.ToolCalls != 1 ||
		execution.Activity.ModelTurns != 1 {
		t.Fatalf("execution activity = %#v", execution.Activity)
	}
	finalization := report.Intervals.ParentFinalization
	if finalization.Status != analysisStatusOpen || finalization.Tokens.Status != analysisStatusOpen ||
		finalization.Tokens.InputTokens != 0 {
		t.Fatalf("finalization interval = %#v", finalization)
	}
}

func runParentUsageReport(t *testing.T, cfg config.AppConfig) parentUsageReport {
	t.Helper()
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeParentUsage}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var report parentUsageReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func parentUsagePhaseLines(t *testing.T, start, completeAt time.Time) []string {
	t.Helper()
	return []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, start.Add(time.Minute), 1500, 700, 300, 200, 2300),
		parentUsageToolCallLine(t, start.Add(2*time.Minute), "parent-usage-exec"),
		parentUsageToolOutputLine(t, start.Add(3*time.Minute), "parent-usage-exec", "0123456789"),
		parentUsageCompactedLine(t, start.Add(4*time.Minute)),
		parentUsageTokenCountLine(t, completeAt.Add(-time.Second), 2000, 1000, 400, 240, 3000),
		parentUsageTokenCountLine(t, completeAt.Add(30*time.Second), 2600, 1300, 480, 260, 3800),
		parentUsageCustomToolCallLine(t, completeAt.Add(time.Minute), "parent-usage-final"),
		parentUsageCustomToolOutputLine(t, completeAt.Add(90*time.Second), "parent-usage-final", "abcd", "efghij"),
		analysisTurnLine(t, completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
		analysisTurnLine(t, completeAt.Add(5*time.Minute), codexRolloutTaskStartedType, analysisLaterTurnID),
		parentUsageTokenCountLine(t, completeAt.Add(6*time.Minute), 3200, 1600, 560, 300, 4200),
	}
}

func parentUsageTokenCountLine(t *testing.T, timestamp time.Time, input, cached, output, reasoning, total int64) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type": "token_count",
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"input_tokens":            input,
				"cached_input_tokens":     cached,
				"output_tokens":           output,
				"reasoning_output_tokens": reasoning,
				"total_tokens":            total,
			},
		},
	})
}

func parentUsageToolCallLine(t *testing.T, timestamp time.Time, callID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type": "function_call", "name": "shell", "call_id": callID, "arguments": "{}",
	})
}

func parentUsageToolOutputLine(t *testing.T, timestamp time.Time, callID, output string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type": "function_call_output", "call_id": callID, "output": output,
	})
}

func parentUsageCustomToolCallLine(t *testing.T, timestamp time.Time, callID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type": "custom_tool_call", "call_id": callID,
	})
}

func parentUsageCustomToolOutputLine(t *testing.T, timestamp time.Time, callID string, texts ...string) string {
	t.Helper()
	items := make([]map[string]any, 0, len(texts))
	for _, text := range texts {
		items = append(items, map[string]any{"type": "input_text", "text": text})
	}
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type": "custom_tool_call_output", "call_id": callID, "output": items,
	})
}

func parentUsageCompactedLine(t *testing.T, timestamp time.Time) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "compacted", map[string]any{"message": ""})
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func parentUsageStateSnapshot(t *testing.T, st *state.StateStore) []byte {
	t.Helper()
	var combined bytes.Buffer
	root := st.Path(".")
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, filePath)
		if relErr != nil {
			return nil
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil
		}
		combined.WriteString(filepath.ToSlash(rel))
		combined.WriteByte('\n')
		combined.Write(data)
		combined.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return combined.Bytes()
}
