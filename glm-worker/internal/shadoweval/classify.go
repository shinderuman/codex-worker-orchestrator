package shadoweval

import (
	"fmt"
)

type UsageMetrics struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type CallMetrics struct {
	DurationMS    int64        `json:"duration_ms"`
	DurationAPIMS int64        `json:"duration_api_ms,omitempty"`
	TotalCostUSD  float64      `json:"total_cost_usd,omitempty"`
	InputBytes    int          `json:"input_bytes"`
	Usage         UsageMetrics `json:"usage"`
}

const ModelAlias = "opus"

const ModelEffort = "low"

const classificationPromptHead = "Dogfood bundle の各 items を独立に shadow 分類してください。必ず全%d件に対し事前指定の型だけを返してください。これは監査・ルーティングの権限を持たないshadow評価です。低確信なら確率に反映してください。入力JSON:\n"

func ClassificationPrompt(input ShadowInput, marshaled []byte) string {
	return fmt.Sprintf(classificationPromptHead, len(input.Items)) + string(marshaled)
}
