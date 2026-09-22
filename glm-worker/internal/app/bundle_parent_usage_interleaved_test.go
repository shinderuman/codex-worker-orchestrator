package app

import (
	"testing"
	"time"
)

func TestInterleavedUnattributedTurnFailsClosedAcrossParentUsage(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	ownedComplete := task.start.Add(10 * time.Minute)
	laterStart := task.start.Add(15 * time.Minute)
	laterComplete := task.start.Add(25 * time.Minute)

	lines := []string{
		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 100, 50, 10, 5, 115),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, ownedComplete.Add(-time.Second), 2628387, 2598066, 1000, 200, 3000000),
		analysisTurnLine(t, ownedComplete, codexRolloutTaskCompleteType, analysisOwningTurnID),
		analysisTurnLine(t, laterStart, codexRolloutTaskStartedType, analysisLaterTurnID),
		parentUsageToolCallLine(t, laterStart.Add(time.Minute), "unattributed-tool"),
		parentUsageTokenCountLine(t, laterComplete.Add(-time.Second), 8869351, 8756914, 2000, 400, 10000000),
		analysisTurnLine(t, laterComplete, codexRolloutTaskCompleteType, analysisLaterTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	execution := report.Intervals.TaskExecution
	if execution.Tokens.Status != codexStatusAmbiguous || execution.Tokens.Reason != parentUsageReasonInterleavedUnattributed ||
		execution.Tokens.InputTokens != 0 || execution.Tokens.CachedInputTokens != 0 {
		t.Fatalf("execution tokens = %#v", execution.Tokens)
	}
	if execution.Activity.Status != codexStatusAmbiguous || execution.Activity.Reason != parentUsageReasonInterleavedUnattributed ||
		execution.Activity.ModelTurns != 0 || execution.Activity.ToolCalls != 0 {
		t.Fatalf("execution activity = %#v", execution.Activity)
	}

	index := runAnalysisBundle(t, task.cfg, "")
	if index.TokenDelta.Status != codexStatusAmbiguous || index.TokenDelta.InputTokens != 0 || index.TokenDelta.CachedInputTokens != 0 {
		t.Fatalf("analysis execution token delta = %#v", index.TokenDelta)
	}
	if index.Intervals.SubsequentRequests.Status != analysisStatusAvailable || len(index.Intervals.SubsequentRequests.Turns) != 1 {
		t.Fatalf("subsequent requests = %#v", index.Intervals.SubsequentRequests)
	}
	later := index.Intervals.SubsequentRequests.Turns[0]
	if later.TurnID != analysisLaterTurnID || later.InputTokens != 6240964 || later.CachedInputTokens != 6158848 {
		t.Fatalf("unattributed later turn = %#v", later)
	}

	stats, err := task.st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	usage := buildTelemetryCompactParentUsage(task.cfg, task.st, stats)
	if usage.Tasks != 1 || usage.Available != 0 || usage.ByStatus[codexStatusIncluded+"/"+codexStatusAmbiguous] != 1 {
		t.Fatalf("compact parent usage coverage = %#v", usage)
	}
	if usage.TaskExecution.Tokens.TasksSummed != 0 || usage.TaskExecution.Tokens.InputTokens != 0 ||
		usage.TaskExecution.Tokens.CachedInputTokens != 0 || usage.TaskExecution.TokensExcludedByStatus[codexStatusAmbiguous] != 1 ||
		usage.TaskExecution.TokensExcludedByReason[parentUsageReasonInterleavedUnattributed] != 1 {
		t.Fatalf("compact token aggregate = %#v", usage.TaskExecution)
	}
	if usage.TaskExecution.Activity.TasksCounted != 0 || usage.TaskExecution.Activity.ModelTurns != 0 ||
		usage.TaskExecution.Activity.ToolCalls != 0 || usage.TaskExecution.ActivityExcludedByStatus[codexStatusAmbiguous] != 1 ||
		usage.TaskExecution.ActivityExcludedByReason[parentUsageReasonInterleavedUnattributed] != 1 {
		t.Fatalf("compact activity aggregate = %#v", usage.TaskExecution)
	}
}

func TestSameTurnInterleavedUserMessageFailsClosedAcrossParentUsage(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	turnStart := task.start.Add(-2 * time.Minute)
	turnComplete := task.completeAt.Add(2 * time.Minute)

	lines := []string{
		analysisTurnLine(t, turnStart, codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisUserMessageLine(t, task.start.Add(-90*time.Second)),
		parentUsageTokenCountLine(t, task.start.Add(-time.Second), 100, 50, 10, 5, 115),
		parentUsageToolCallLine(t, task.start.Add(time.Minute), "task-tool"),
		analysisUserMessageLine(t, task.start.Add(5*time.Minute)),
		parentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 1000, 500, 160, 80, 1500),
		analysisTurnLine(t, turnComplete, codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	execution := report.Intervals.TaskExecution
	if execution.Tokens.Status != codexStatusAmbiguous || execution.Tokens.Reason != parentUsageReasonSameTurnInterleaved ||
		execution.Tokens.InputTokens != 0 || execution.Tokens.CachedInputTokens != 0 {
		t.Fatalf("execution tokens = %#v", execution.Tokens)
	}
	if execution.Activity.Status != codexStatusAmbiguous || execution.Activity.Reason != parentUsageReasonSameTurnInterleaved ||
		execution.Activity.ModelTurns != 0 || execution.Activity.ToolCalls != 0 {
		t.Fatalf("execution activity = %#v", execution.Activity)
	}

	index := runAnalysisBundle(t, task.cfg, "")
	if index.TokenDelta.Status != codexStatusAmbiguous || index.TokenDelta.InputTokens != 0 || index.TokenDelta.CachedInputTokens != 0 {
		t.Fatalf("analysis execution token delta = %#v", index.TokenDelta)
	}
	if index.Intervals.SubsequentRequests.Status != analysisStatusAvailable || len(index.Intervals.SubsequentRequests.Turns) != 0 {
		t.Fatalf("subsequent requests = %#v", index.Intervals.SubsequentRequests)
	}

	stats, err := task.st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	usage := buildTelemetryCompactParentUsage(task.cfg, task.st, stats)
	if usage.Available != 0 || usage.TaskExecution.Tokens.TasksSummed != 0 || usage.TaskExecution.Activity.TasksCounted != 0 ||
		usage.TaskExecution.TokensExcludedByReason[parentUsageReasonSameTurnInterleaved] != 1 ||
		usage.TaskExecution.ActivityExcludedByReason[parentUsageReasonSameTurnInterleaved] != 1 {
		t.Fatalf("compact parent usage = %#v", usage)
	}
}

func TestSameTurnInterleavingAfterExecutionDoesNotInvalidateExecutionInterval(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	turnStart := task.start.Add(-2 * time.Minute)
	turnComplete := task.completeAt.Add(4 * time.Minute)
	lines := []string{
		analysisTurnLine(t, turnStart, codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisUserMessageLine(t, task.start.Add(-90*time.Second)),
		parentUsageTokenCountLine(t, task.start.Add(-time.Second), 100, 50, 10, 5, 115),
		parentUsageToolCallLine(t, task.start.Add(time.Minute), "task-tool"),
		parentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 1000, 500, 160, 80, 1500),
		analysisUserMessageLine(t, task.completeAt.Add(time.Minute)),
		analysisTurnLine(t, turnComplete, codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID, task.start.Add(-3*time.Hour), lines)
	report := runParentUsageReport(t, task.cfg)
	if report.Intervals.TaskExecution.Tokens.Status != analysisStatusAvailable || report.Intervals.TaskExecution.Activity.Status != analysisStatusCounted {
		t.Fatalf("execution interval = %#v", report.Intervals.TaskExecution)
	}
}

func analysisUserMessageLine(t *testing.T, timestamp time.Time) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type":    codexRolloutUserMessageType,
		"message": "opaque-user-message",
		"kind":    "plain",
	})
}
