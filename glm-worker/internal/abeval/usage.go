package abeval

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func GLMUsageFromTaskStats(stats state.TaskStats) (GLMUsage, ProxyMetrics) {
	usage := GLMUsage{
		Source:                   GLMUsageSourceTaskStats,
		TaskID:                   stats.TaskID,
		InputTokens:              sumInt64Map(stats.InputTokensByAlias),
		CacheCreationInputTokens: sumInt64Map(stats.CacheCreationInputTokensByAlias),
		CacheReadInputTokens:     sumInt64Map(stats.CacheReadInputTokensByAlias),
		OutputTokens:             sumInt64Map(stats.OutputTokensByAlias),
		ModelCalls:               stats.ModelCalls,
	}
	proxy := ProxyMetrics{
		SolPacketBytes:      stats.SolPacketBytes,
		SolDecisionCommands: stats.DecisionCommands,
		SolFixCommands:      stats.FixCommands,
		AutoFixRounds:       stats.AutoFixRounds,
	}
	return usage, proxy
}

func ResolveFromTaskStats(record RunRecord, all []state.TaskStats) (RunRecord, error) {
	if record.GLMUsage.TaskID == "" {
		return record, fmt.Errorf("glm_usageを解決できません: task_idが空です")
	}
	for _, stats := range all {
		if stats.TaskID != record.GLMUsage.TaskID {
			continue
		}
		usage, proxy := GLMUsageFromTaskStats(stats)
		record.GLMUsage = usage
		record.Proxy = proxy
		return record, nil
	}
	return record, fmt.Errorf("glm_usageを解決できません: task %sのstatsが見つかりません", record.GLMUsage.TaskID)
}

func sumInt64Map(values map[string]int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}
