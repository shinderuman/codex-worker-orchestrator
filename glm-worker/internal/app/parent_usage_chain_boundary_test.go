package app

import (
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentUsageChainBoundaryAggregation(t *testing.T) {
	cases := []struct {
		name          string
		secondAnchors [][6]int64
		wantInput     int64
		wantCached    int64
		wantOutput    int64
		wantReasoning int64
		wantTotal     int64
	}{
		{
			name: "carried cumulative counter contributes only the segment delta",
			secondAnchors: [][6]int64{
				{1100, 550, 260, 170, 1700, 130},
				{1200, 600, 280, 180, 1900, 140},
			},
			wantInput:     200,
			wantCached:    100,
			wantOutput:    40,
			wantReasoning: 20,
			wantTotal:     400,
		},
		{
			name: "self-contained restart contributes its absolute counters",
			secondAnchors: [][6]int64{
				{50, 20, 10, 5, 80, 80},
				{150, 60, 20, 10, 240, 240},
			},
			wantInput:     250,
			wantCached:    110,
			wantOutput:    40,
			wantReasoning: 20,
			wantTotal:     440,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := newAnalysisTerminalTask(t)
			report := buildChainBoundaryReport(t, task, tc.secondAnchors)
			if report.ParentSession.Status != codexStatusIncluded {
				t.Fatalf("parent session = %#v", report.ParentSession)
			}
			if len(report.ParentSession.RolloutChain) != 2 {
				t.Fatalf("rollout chain = %#v", report.ParentSession.RolloutChain)
			}
			tokens := report.Intervals.TaskExecution.Tokens
			if tokens.Status != analysisStatusAvailable || tokens.InputTokens != tc.wantInput || tokens.CachedInputTokens != tc.wantCached || tokens.OutputTokens != tc.wantOutput || tokens.ReasoningTokens != tc.wantReasoning || tokens.TotalTokens != tc.wantTotal {
				t.Fatalf("tokens = %#v", tokens)
			}
		})
	}
}

func TestParentUsageChainBoundaryForwardJumpStaysAmbiguous(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	report := buildChainBoundaryReport(t, task, [][6]int64{
		{1101, 551, 261, 171, 1701, 130},
		{1200, 600, 280, 180, 1900, 140},
	})
	if report.ParentSession.Status != codexStatusAmbiguous || !strings.Contains(report.ParentSession.Detail, "advances beyond the previous segment token counter") {
		t.Fatalf("parent session = %#v", report.ParentSession)
	}
	if report.Intervals.TaskExecution.Tokens.Status != codexStatusAmbiguous {
		t.Fatalf("ambiguous boundary produced token totals: %#v", report.Intervals.TaskExecution.Tokens)
	}
}

func buildChainBoundaryReport(t *testing.T, task analysisTerminalTask, secondAnchors [][6]int64) parentUsageReport {
	t.Helper()
	firstSegment := []string{
		chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		chainTokenCountLine(t, task.start.Add(10*time.Minute), 1100, 550, 260, 170, 1700, 130),
		analysisTurnLine(t, task.start.Add(12*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	secondSegment := make([]string, len(secondAnchors))
	for i, anchor := range secondAnchors {
		at := task.start.Add(time.Duration(20+i*5) * time.Minute)
		secondSegment[i] = chainTokenCountLine(t, at, anchor[0], anchor[1], anchor[2], anchor[3], anchor[4], anchor[5])
	}
	lastRel := "archived_sessions/rollout-last-" + codexTestParentThreadID + ".jsonl"
	writeChainRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID, task.start.Add(-2*time.Minute), "/repo", "Codex Desktop", firstSegment)
	writeChainRollout(t, task.codexHome, lastRel, codexTestParentThreadID, task.start.Add(15*time.Minute), "/repo", "Codex Desktop", secondSegment)
	stats, err := task.st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	var selected state.TaskStats
	for _, record := range stats {
		if record.TaskID == task.taskID {
			selected = record
		}
	}
	return buildParentUsageReport(task.cfg, task.st, bundleTask{ID: task.taskID, Status: string(state.TaskStatusComplete), Stats: selected})
}
