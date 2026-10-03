package app

import (
	"encoding/json"

	"path/filepath"

	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type analysisTerminalTask struct {
	cfg        config.AppConfig
	st         *state.StateStore
	codexHome  string
	taskID     string
	start      time.Time
	completeAt time.Time
}

const analysisOwningTurnID = "turn-owning"

const analysisLaterTurnID = "turn-later"

func TestScanCodexRolloutWindowRejectsMalformedRecords(t *testing.T) {
	start := time.Date(2026, 9, 4, 1, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	cases := []struct {
		name        string
		lines       string
		wantLocator string
	}{
		{
			name:        "malformed-json-record",
			lines:       analysisTokenCountLine(t, start.Add(time.Minute), 100, 50) + "{malformed\n",
			wantLocator: "2行目",
		},
		{
			name: "invalid-timestamp-record",
			lines: analysisTokenCountLine(t, start.Add(time.Minute), 100, 50) +
				`{"timestamp":"not-a-timestamp","type":"event_msg","payload":{}}` + "\n",
			wantLocator: "2行目",
		},
		{
			name: "blank-line-and-unrelated-record-tolerated",
			lines: "\n" + analysisTokenCountLine(t, start.Add(time.Minute), 100, 50) +
				analysisRolloutLine(t, start.Add(2*time.Minute), "unknown_record_type", map[string]any{"detail": true}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			writeBundleFile(t, path, tc.lines)
			scan, err := scanCodexRolloutWindow(path, start, end)
			if tc.wantLocator == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(scan.tokens) != 1 || !scan.hasWindow {
					t.Fatalf("tolerated scan = %#v", scan)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantLocator) {
				t.Fatalf("scan error = %v", err)
			}
		})
	}
}

func newAnalysisTerminalTask(t *testing.T) analysisTerminalTask {
	t.Helper()
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID, nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-2 * time.Hour)
	completeAt := start.Add(30 * time.Minute)
	analysisRetireCurrentTask(t, st, taskID, start, completeAt)
	return analysisTerminalTask{cfg: cfg, st: st, codexHome: codexHome, taskID: taskID, start: start, completeAt: completeAt}
}

func analysisRetireCurrentTask(t *testing.T, st *state.StateStore, taskID string, start, completeAt time.Time) {
	t.Helper()
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
		stats.Status = state.TaskStatusComplete
	})
	if err := st.AppendTaskLifecycle(state.TaskLifecycleRecord{
		Version:   1,
		TaskID:    taskID,
		Timestamp: completeAt,
		From:      string(state.TaskStatusActive),
		To:        string(state.TaskStatusComplete),
	}); err != nil {
		t.Fatal(err)
	}
}

func analysisRolloutRel() string {
	return "sessions/2026/08/30/rollout-live-" + codexTestParentThreadID + ".jsonl"
}

func analysisTurnLine(t *testing.T, timestamp time.Time, eventType, turnID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{"type": eventType, "turn_id": turnID})
}

func analysisRolloutLine(t *testing.T, timestamp time.Time, recordType string, payload map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": timestamp.UTC().Format(time.RFC3339Nano),
		"type":      recordType,
		"payload":   payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func analysisTokenCountLine(t *testing.T, timestamp time.Time, input, cached int64) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type": "token_count",
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"input_tokens":        input,
				"cached_input_tokens": cached,
			},
		},
	})
}

func writeAnalysisRollout(t *testing.T, home, rel, threadID string, metaTimestamp time.Time, lines []string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": metaTimestamp.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id": threadID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(home, filepath.FromSlash(rel)), string(encoded)+"\n"+strings.Join(lines, ""))
}
