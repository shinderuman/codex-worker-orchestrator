package app

import (
	"testing"
	"time"
)

func TestParentUsageCounterResetThenRecoveryStillReportsReset(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	lines := []string{
		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, task.start.Add(10*time.Minute), 100, 50, 20, 10, 180),
		parentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 2000, 1000, 480, 320, 3000),
		analysisTurnLine(t, task.completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	report := runParentUsageReport(t, task.cfg)
	tokens := report.Intervals.TaskExecution.Tokens
	if tokens.Status != analysisStatusCounterReset {
		t.Fatalf("execution tokens = %#v", tokens)
	}
	if tokens.InputTokens != 0 || tokens.CachedInputTokens != 0 || tokens.OutputTokens != 0 ||
		tokens.ReasoningTokens != 0 || tokens.TotalTokens != 0 {
		t.Fatalf("counter reset carries token deltas: %#v", tokens)
	}
	if tokens.BaselineSource != parentUsageSourceLocator(analysisRolloutRel(), 2) ||
		tokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 5) {
		t.Fatalf("counter reset locators = %#v", tokens)
	}
}
