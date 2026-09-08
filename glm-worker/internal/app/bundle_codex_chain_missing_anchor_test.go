package app

import (
	"strings"
	"testing"
	"time"
)

func TestParentUsageRolloutChainRejectsMemberWithoutTokenAnchors(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	firstRel := analysisRolloutRel()
	secondRel := "archived_sessions/rollout-no-token-anchor-" + codexTestParentThreadID + ".jsonl"
	splitAt := task.start.Add(10 * time.Minute)
	writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, task.start.Add(-3*time.Hour), "/repo", "Codex Desktop", []string{
		chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		chainTokenCountLine(t, task.start.Add(5*time.Minute), 1500, 700, 300, 200, 2300, 800),
	})
	writeChainRollout(t, task.codexHome, secondRel, codexTestParentThreadID, splitAt, "/repo", "Codex Desktop", []string{
		parentUsageToolCallLine(t, splitAt.Add(time.Minute), "no-anchor-tool-call"),
		analysisTurnLine(t, task.completeAt, codexRolloutTaskCompleteType, analysisOwningTurnID),
	})

	report := runParentUsageReport(t, task.cfg)
	if report.ParentSession.Status != codexStatusAmbiguous {
		t.Fatalf("parent session = %#v", report.ParentSession)
	}
	if !strings.Contains(report.ParentSession.Detail, "no usable token counter anchor") {
		t.Fatalf("parent detail = %q", report.ParentSession.Detail)
	}
	if report.Intervals.TaskExecution.Tokens.Status != codexStatusAmbiguous {
		t.Fatalf("task execution tokens = %#v", report.Intervals.TaskExecution.Tokens)
	}
}
