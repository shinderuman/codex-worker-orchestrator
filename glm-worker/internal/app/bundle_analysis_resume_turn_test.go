package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBundleAnalysisAutoResumeTurnIsTaskOwned(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	resumeStart := task.completeAt.Add(-10 * time.Minute)
	resumeComplete := task.completeAt.Add(2 * time.Minute)
	laterStart := task.completeAt.Add(4 * time.Minute)
	laterComplete := task.completeAt.Add(5 * time.Minute)

	lines := []string{
		analysisTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisTokenCountLine(t, task.start.Add(2*time.Minute), 1500, 700),
		analysisTurnLine(t, task.start.Add(5*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
		analysisTurnLine(t, resumeStart, codexRolloutTaskStartedType, "turn-resume"),
		analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", task.taskID, true, true),
		analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{"resumed":true}`),
		analysisTokenCountLine(t, task.completeAt.Add(-time.Second), 2200, 1100),
		analysisTokenCountLine(t, task.completeAt.Add(30*time.Second), 2500, 1300),
		analysisTurnLine(t, resumeComplete, codexRolloutTaskCompleteType, "turn-resume"),
		analysisTurnLine(t, laterStart, codexRolloutTaskStartedType, analysisLaterTurnID),
		analysisTokenCountLine(t, laterStart.Add(30*time.Second), 2900, 1500),
		analysisTurnLine(t, laterComplete, codexRolloutTaskCompleteType, analysisLaterTurnID),
	}
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), lines)

	index := runAnalysisBundle(t, task.cfg, "")
	finalization := index.Intervals.ParentFinalization
	if finalization.Status != analysisStatusAvailable || finalization.Start == nil || finalization.End == nil ||
		*finalization.Start != task.completeAt.Format(time.RFC3339Nano) ||
		*finalization.End != resumeComplete.Format(time.RFC3339Nano) {
		t.Fatalf("auto-resume finalization interval = %#v", finalization)
	}
	if index.Finalization.Status != analysisStatusAvailable || index.Finalization.InputTokens != 300 ||
		index.Finalization.CachedInputTokens != 200 {
		t.Fatalf("auto-resume finalization token delta = %#v", index.Finalization)
	}
	if index.TokenDelta.Status != analysisStatusAvailable || index.TokenDelta.InputTokens != 1200 ||
		index.TokenDelta.CachedInputTokens != 600 {
		t.Fatalf("auto-resume execution token delta = %#v", index.TokenDelta)
	}
	subsequent := index.Intervals.SubsequentRequests
	if subsequent.Status != analysisStatusAvailable || len(subsequent.Turns) != 1 ||
		subsequent.Turns[0].TurnID != analysisLaterTurnID {
		t.Fatalf("auto-resume subsequent requests = %#v", subsequent)
	}
	if subsequent.Turns[0].InputTokens != 400 || subsequent.Turns[0].CachedInputTokens != 200 {
		t.Fatalf("later unrelated turn token delta = %#v", subsequent.Turns[0])
	}
}

func TestResolveAnalysisOwningTurnRequiresExactResumeEvidence(t *testing.T) {
	start := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	taskID := "task-exact"
	initialStart := start.Add(-time.Minute)
	initialEnd := start.Add(2 * time.Minute)
	resumeStart := start.Add(10 * time.Minute)

	cases := []struct {
		name       string
		commands   []string
		wantStatus string
		wantTurns  int
	}{
		{
			name: "exact status then successful resume",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", taskID, true, true),
				analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{}`),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  2,
		},
		{
			name: "wrong task id remains unrelated",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", "other-task", true, true),
				analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{}`),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  1,
		},
		{
			name: "missing resume command remains unrelated",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", taskID, true, true),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  1,
		},
		{
			name: "failed resume remains unrelated",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", taskID, true, true),
				analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 1, `{}`),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  1,
		},
		{
			name: "resume before status remains unrelated",
			commands: []string{
				analysisCommandCompletedLine(t, resumeStart.Add(time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{}`),
				analysisResumeStatusLine(t, resumeStart.Add(2*time.Minute), "turn-resume", taskID, true, true),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  1,
		},
		{
			name: "non resumable status remains unrelated",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", taskID, true, false),
				analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{}`),
			},
			wantStatus: analysisStatusAvailable,
			wantTurns:  1,
		},
		{
			name: "conflicting eligible task ids fail closed",
			commands: []string{
				analysisResumeStatusLine(t, resumeStart.Add(time.Minute), "turn-resume", taskID, true, true),
				analysisResumeStatusLine(t, resumeStart.Add(90*time.Second), "turn-resume", "other-task", true, true),
				analysisCommandCompletedLine(t, resumeStart.Add(2*time.Minute), "turn-resume", analysisParentResumeCommand, 0, `{}`),
			},
			wantStatus: analysisStatusUnknown,
			wantTurns:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{
				analysisTurnLine(t, initialStart, codexRolloutTaskStartedType, "turn-initial"),
				analysisTurnLine(t, initialEnd, codexRolloutTaskCompleteType, "turn-initial"),
				analysisTurnLine(t, resumeStart, codexRolloutTaskStartedType, "turn-resume"),
			}
			lines = append(lines, tc.commands...)
			lines = append(lines, analysisTurnLine(t, resumeStart.Add(3*time.Minute), codexRolloutTaskCompleteType, "turn-resume"))
			path := t.TempDir() + "/rollout.jsonl"
			writeBundleFile(t, path, joinAnalysisLines(lines))
			scan, err := scanCodexRolloutWindow(path, start.Add(-time.Hour), start.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			association := codexAssociation{ParentStatus: codexStatusIncluded, ParentPath: path}
			ownership := resolveAnalysisTaskOwnership(association, scan.turns, start, start.Add(time.Hour), taskID)
			if ownership.status != tc.wantStatus {
				t.Fatalf("owning status = %q want %q: %#v", ownership.status, tc.wantStatus, ownership)
			}
			if tc.wantStatus == analysisStatusAvailable && len(ownership.owned) != tc.wantTurns {
				t.Fatalf("owned turns = %#v want %d", ownership.owned, tc.wantTurns)
			}
		})
	}
}

func analysisResumeStatusLine(t *testing.T, timestamp time.Time, turnID, taskID string, limited, resumeAvailable bool) string {
	t.Helper()
	status, err := json.Marshal(map[string]any{
		"task_id":          taskID,
		"task_status":      "rate-limited",
		"rate_limited":     map[string]any{"limited": limited},
		"resume_available": resumeAvailable,
	})
	if err != nil {
		t.Fatal(err)
	}
	return analysisCommandCompletedLine(t, timestamp, turnID, analysisWorkerStatusCommand, 0, string(status))
}

func analysisCommandCompletedLine(t *testing.T, timestamp time.Time, turnID, command string, exitCode int, stdout string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type":    codexRolloutItemCompletedType,
		"turn_id": turnID,
		"item": map[string]any{
			"type":      codexRolloutCommandExecutionType,
			"command":   []string{"/bin/zsh", "-lc", command},
			"exit_code": exitCode,
			"stdout":    stdout,
		},
	})
}

func joinAnalysisLines(lines []string) string {
	result := ""
	for _, line := range lines {
		result += line
	}
	return result
}
