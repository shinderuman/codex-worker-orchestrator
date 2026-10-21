package bundleanalysis

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentUsageSummary struct {
	Tasks              int                     `json:"tasks"`
	Available          int                     `json:"available"`
	Ambiguous          int                     `json:"ambiguous"`
	Unknown            int                     `json:"unknown"`
	ByStatus           map[string]int          `json:"by_status"`
	TaskExecution      ParentUsageIntervalView `json:"task_execution"`
	ParentFinalization ParentUsageIntervalView `json:"parent_finalization"`
}

type ParentUsageIntervalView struct {
	Tokens                   ParentUsageTokensView   `json:"tokens"`
	Activity                 ParentUsageActivityView `json:"activity"`
	TokensExcludedByStatus   map[string]int          `json:"tokens_excluded_by_status"`
	ActivityExcludedByStatus map[string]int          `json:"activity_excluded_by_status"`
	TokensExcludedByReason   map[string]int          `json:"tokens_excluded_by_reason,omitempty"`
	ActivityExcludedByReason map[string]int          `json:"activity_excluded_by_reason,omitempty"`
}

type ParentUsageTokensView struct {
	TasksSummed       int   `json:"tasks_summed"`
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	ReasoningTokens   int64 `json:"reasoning_output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

type ParentUsageActivityView struct {
	TasksCounted    int   `json:"tasks_counted"`
	ModelTurns      int   `json:"model_turns"`
	ToolCalls       int   `json:"tool_calls"`
	ToolResults     int   `json:"tool_results"`
	Compactions     int   `json:"compactions"`
	ToolOutputBytes int64 `json:"tool_output_bytes"`
}

const parentUsageMissingTokenField = "missing-token-field"

func BuildParentUsageSummary(cfg config.AppConfig, st *state.StateStore, filtered []state.TaskStats) ParentUsageSummary {
	return buildParentUsageSummaryWithScans(cfg, st, filtered, scanCodexRollouts, scanCodexRolloutChainWindow)
}

func buildParentUsageSummaryWithScans(cfg config.AppConfig, st *state.StateStore, filtered []state.TaskStats, enumerate func(string) ([]codexRollout, error), scanChain func([]codexRollout, time.Time, time.Time) (bundleRolloutScan, error)) ParentUsageSummary {
	usage := ParentUsageSummary{
		ByStatus:           make(map[string]int),
		TaskExecution:      newParentUsageIntervalView(),
		ParentFinalization: newParentUsageIntervalView(),
	}
	batch := newParentUsageBatch(cfg.CodexConfigDir, filtered, enumerate, scanChain)
	for _, stats := range filtered {
		task := bundleTask{ID: stats.TaskID, Status: string(stats.Status), Stats: stats}
		evidence := batch.evidence(task)
		report := buildParentUsageReportFromScan(st, task, evidence.association, evidence.scan, evidence.err)
		usage.ByStatus[report.ParentSession.Status+"/"+report.Intervals.TaskExecution.Tokens.Status]++
		usage.TaskExecution.accumulate(report.Intervals.TaskExecution)
		usage.ParentFinalization.accumulate(report.Intervals.ParentFinalization)
		switch {
		case report.ParentSession.Status == codexStatusAmbiguous:
			usage.Ambiguous++
		case report.ParentSession.Status == codexStatusIncluded && report.Intervals.TaskExecution.Tokens.Status == analysisStatusAvailable:
			usage.Available++
		default:
			usage.Unknown++
		}
	}
	usage.Tasks = len(filtered)
	return usage
}

func newParentUsageIntervalView() ParentUsageIntervalView {
	return ParentUsageIntervalView{
		TokensExcludedByStatus:   make(map[string]int),
		ActivityExcludedByStatus: make(map[string]int),
		TokensExcludedByReason:   make(map[string]int),
		ActivityExcludedByReason: make(map[string]int),
	}
}

func (interval *ParentUsageIntervalView) accumulate(parent parentUsageInterval) {
	if parentUsageTokensSummable(parent.Tokens) {
		interval.Tokens.TasksSummed++
		interval.Tokens.InputTokens += parent.Tokens.InputTokens
		interval.Tokens.CachedInputTokens += parent.Tokens.CachedInputTokens
		interval.Tokens.OutputTokens += parent.Tokens.OutputTokens
		interval.Tokens.ReasoningTokens += parent.Tokens.ReasoningTokens
		interval.Tokens.TotalTokens += parent.Tokens.TotalTokens
	} else {
		interval.TokensExcludedByStatus[parentUsageTokenExclusionStatus(parent.Tokens)]++
		if reason := parentUsageTokenExclusionReason(parent.Tokens); reason != "" {
			interval.TokensExcludedByReason[reason]++
		}
	}
	if parent.Activity.Status != analysisStatusCounted {
		interval.ActivityExcludedByStatus[parent.Activity.Status]++
		if parent.Activity.Reason != "" {
			interval.ActivityExcludedByReason[parent.Activity.Reason]++
		}
		return
	}
	interval.Activity.TasksCounted++
	interval.Activity.ModelTurns += parent.Activity.ModelTurns
	interval.Activity.ToolCalls += parent.Activity.ToolCalls
	interval.Activity.ToolResults += parent.Activity.ToolResults
	interval.Activity.Compactions += parent.Activity.Compactions
	interval.Activity.ToolOutputBytes += parent.Activity.ToolOutputBytes
}

func parentUsageTokensSummable(tokens parentUsageTokens) bool {
	return tokens.Status == analysisStatusAvailable && len(tokens.UnknownFields) == 0
}

func parentUsageTokenExclusionStatus(tokens parentUsageTokens) string {
	if tokens.Status == analysisStatusAvailable {
		return parentUsageMissingTokenField
	}
	return tokens.Status
}

func parentUsageTokenExclusionReason(tokens parentUsageTokens) string {
	if tokens.Reason != "" {
		return tokens.Reason
	}
	if tokens.Status == analysisStatusAvailable && len(tokens.UnknownFields) != 0 {
		return parentUsageMissingTokenField
	}
	return ""
}
