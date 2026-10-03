package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeChainRollout(t *testing.T, home, rel, threadID string, metaTimestamp time.Time, cwd, originator string, lines []string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": metaTimestamp.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":         threadID,
			"cwd":        cwd,
			"originator": originator,
			"source":     "vscode",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(home, filepath.FromSlash(rel)), string(encoded)+"\n"+strings.Join(lines, ""))
}

func chainTokenCountLine(t *testing.T, timestamp time.Time, input, cached, output, reasoning, total, lastTotal int64) string {
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
			"last_token_usage": map[string]any{
				"input_tokens": lastTotal,
				"total_tokens": lastTotal,
			},
		},
	})
}
