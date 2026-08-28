package runner

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

type streamCompactMetadata struct {
	Trigger                 string `json:"trigger"`
	PreTokens               int64  `json:"preTokens"`
	PostTokens              int64  `json:"postTokens"`
	CumulativeDroppedTokens int64  `json:"cumulativeDroppedTokens"`
	DurationMS              int64  `json:"durationMs"`
}

func reduceCompactionMetadata(metadata *streamCompactMetadata) *state.TaskCompactionSummary {
	if metadata == nil {
		return nil
	}
	return &state.TaskCompactionSummary{
		Trigger:                 sanitizedCompactionTrigger(metadata.Trigger),
		PreTokens:               metadata.PreTokens,
		PostTokens:              metadata.PostTokens,
		CumulativeDroppedTokens: metadata.CumulativeDroppedTokens,
		DurationMS:              metadata.DurationMS,
	}
}

func sanitizedCompactionTrigger(trigger string) string {
	switch trigger {
	case "auto", "manual":
		return trigger
	default:
		return ""
	}
}
