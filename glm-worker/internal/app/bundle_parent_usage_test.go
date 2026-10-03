package app

import (
	"testing"
	"time"
)

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
