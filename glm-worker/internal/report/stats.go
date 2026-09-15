package report

import (
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type StatsOutput struct {
	Query                                   queryView           `json:"query"`
	Tasks                                   int                 `json:"tasks"`
	ModelCalls                              int                 `json:"model_calls"`
	ModelCallsByAlias                       map[string]int      `json:"model_calls_by_alias"`
	ProbeCalls                              int                 `json:"probe_calls"`
	TotalAICalls                            int                 `json:"total_ai_calls"`
	TelemetryCoverage                       statsCoverage       `json:"telemetry_coverage"`
	Preflight                               statsPreflight      `json:"preflight"`
	ModelDurationMSByAlias                  map[string]int64    `json:"model_duration_ms_by_alias"`
	InputTokensByAlias                      map[string]int64    `json:"input_tokens_by_alias"`
	CacheCreationInputTokensByAlias         map[string]int64    `json:"cache_creation_input_tokens_by_alias"`
	CacheReadInputTokensByAlias             map[string]int64    `json:"cache_read_input_tokens_by_alias"`
	TotalPromptTokensByAlias                map[string]int64    `json:"total_prompt_tokens_by_alias"`
	OutputTokensByAlias                     map[string]int64    `json:"output_tokens_by_alias"`
	TopLevelTurnsByAlias                    map[string]int      `json:"top_level_turns_by_alias"`
	CallTreesByResolvedModel                map[string]int      `json:"call_trees_by_resolved_model"`
	InputTokensByResolvedModel              map[string]int64    `json:"input_tokens_by_resolved_model"`
	CacheCreationInputTokensByResolvedModel map[string]int64    `json:"cache_creation_input_tokens_by_resolved_model"`
	CacheReadInputTokensByResolvedModel     map[string]int64    `json:"cache_read_input_tokens_by_resolved_model"`
	OutputTokensByResolvedModel             map[string]int64    `json:"output_tokens_by_resolved_model"`
	WorkerCalls                             int                 `json:"worker_calls"`
	ReviewerCalls                           int                 `json:"reviewer_calls"`
	DecisionCommands                        int                 `json:"decision_commands"`
	FixCommands                             int                 `json:"fix_commands"`
	ResumeCommands                          int                 `json:"resume_commands"`
	TransientRetries                        int                 `json:"transient_retries"`
	AutoFixRounds                           int                 `json:"auto_fix_rounds"`
	NeedsSolDecisionPackets                 int                 `json:"needs_sol_decision_packets"`
	NeedsSolReviewPackets                   int                 `json:"needs_sol_review_packets"`
	PassPackets                             int                 `json:"pass_packets"`
	RateLimits                              int                 `json:"rate_limits"`
	RateLimitsByAlias                       map[string]int      `json:"rate_limits_by_alias"`
	ProviderUnavailable                     int                 `json:"provider_unavailable"`
	ProviderUnavailableByAlias              map[string]int      `json:"provider_unavailable_by_alias"`
	PacketCompactions                       int                 `json:"packet_compactions"`
	RiskFloorByCategory                     map[string]int      `json:"risk_floor_by_category"`
	SnapshotMismatches                      int                 `json:"snapshot_mismatches"`
	SnapshotMismatchByAxis                  map[string]int      `json:"snapshot_mismatch_by_axis"`
	PacketRejectByCategory                  map[string]int      `json:"packet_reject_by_category"`
	ProbeOutcome                            map[string]int      `json:"probe_outcome"`
	ParentOutcomes                          map[string]int      `json:"parent_outcomes"`
	ParentFixOrigins                        map[string]int      `json:"parent_fix_origins"`
	ParentOutcomesByModel                   map[string]int      `json:"parent_outcomes_by_model"`
	ParentOutcomesByRisk                    map[string]int      `json:"parent_outcomes_by_risk"`
	ParentFixRework                         []statsParentRework `json:"parent_fix_rework"`
	ParentFixReworkCoverage                 string              `json:"parent_fix_rework_coverage"`
	SolPacketBytes                          int                 `json:"sol_packet_bytes"`
	TelemetryDir                            string              `json:"telemetry_dir"`
	CurrentTask                             statsCurrentTask    `json:"current_task"`
}

type statsCoverage struct {
	Status        string              `json:"status"`
	StatsCalls    int                 `json:"stats_calls"`
	RawRecords    int                 `json:"raw_records"`
	MissingCalls  int                 `json:"missing_calls"`
	ExcessRecords int                 `json:"excess_records"`
	OrphanFiles   int                 `json:"orphan_files"`
	UsageKnown    bool                `json:"usage_totals_known"`
	Tasks         []StatsCoverageTask `json:"tasks"`
}

type StatsCoverageTask struct {
	TaskID         string `json:"task_id"`
	Classification string `json:"classification"`
	StatsCalls     int    `json:"stats_calls"`
	RawRecords     int    `json:"raw_records"`
	MissingCalls   int    `json:"missing_calls"`
	ExcessRecords  int    `json:"excess_records"`
}

type statsPreflight struct {
	Status            string     `json:"status"`
	PassCount         int        `json:"pass_count"`
	FailureCount      int        `json:"failure_count"`
	AvoidedModelCalls int        `json:"avoided_model_calls"`
	TotalDurationMS   int64      `json:"total_duration_ms"`
	MaxDurationMS     int64      `json:"max_duration_ms"`
	LastDurationMS    int64      `json:"last_duration_ms"`
	LastOutcome       string     `json:"last_outcome"`
	LastAt            *time.Time `json:"last_at,omitempty"`
	Error             string     `json:"error,omitempty"`
}

type statsParentRework struct {
	Origin           string `json:"origin"`
	Calls            int    `json:"calls"`
	WorkerCalls      int    `json:"worker_calls"`
	ReviewerCalls    int    `json:"reviewer_calls"`
	Turns            int    `json:"turns"`
	TreeInputTokens  int64  `json:"tree_input_tokens"`
	TreeOutputTokens int64  `json:"tree_output_tokens"`
	WallDurationMS   int64  `json:"wall_duration_ms"`
}

type statsCurrentTask struct {
	ID          *string `json:"id"`
	Status      *string `json:"status"`
	ArtifactDir *string `json:"artifact_dir"`
}

type statsHistoryOutput struct {
	Query     queryView                  `json:"query"`
	Telemetry state.TelemetryHistoryScan `json:"telemetry"`
}

type CompactSummaryFunc func(cfg config.AppConfig, st *state.StateStore, query Query, stdout io.Writer) error

func PrintStats(cfg config.AppConfig, st *state.StateStore, query Query, compact CompactSummaryFunc, stdout io.Writer) error {
	if query.Compact {
		return compact(cfg, st, query, stdout)
	}
	if query.IsHistory() {
		return printStatsHistory(st, query, stdout)
	}
	all, err := st.AllTaskStats()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, buildStatsOutput(st, all, query))
}

func printStatsHistory(st *state.StateStore, query Query, stdout io.Writer) error {
	scan, err := st.ScanTelemetryHistory(query.Filter)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, statsHistoryOutput{
		Query:     query.view(QueryPeriodBasisRecord),
		Telemetry: *scan,
	})
}

func NewAggregateTaskStats() state.TaskStats {
	return state.TaskStats{
		ModelCallsByAlias:                       map[string]int{},
		ModelDurationMSByAlias:                  map[string]int64{},
		RateLimitsByAlias:                       map[string]int{},
		InputTokensByAlias:                      map[string]int64{},
		CacheCreationInputTokensByAlias:         map[string]int64{},
		CacheReadInputTokensByAlias:             map[string]int64{},
		OutputTokensByAlias:                     map[string]int64{},
		TopLevelTurnsByAlias:                    map[string]int{},
		CallTreesByResolvedModel:                map[string]int{},
		InputTokensByResolvedModel:              map[string]int64{},
		CacheCreationInputTokensByResolvedModel: map[string]int64{},
		CacheReadInputTokensByResolvedModel:     map[string]int64{},
		OutputTokensByResolvedModel:             map[string]int64{},
		ProviderUnavailableByAlias:              map[string]int{},
		RiskFloorByCategory:                     map[string]int{},
		SnapshotMismatchByAxis:                  map[string]int{},
		PacketRejectByCategory:                  map[string]int{},
		ProbeOutcome:                            map[string]int{},
		ParentOutcomes:                          map[string]int{},
		ParentFixOrigins:                        map[string]int{},
		ParentOutcomesByModel:                   map[string]int{},
		ParentOutcomesByRisk:                    map[string]int{},
	}
}

func MergeTaskStats(aggregate *state.TaskStats, stats state.TaskStats) {
	aggregate.ModelCalls += stats.ModelCalls
	mergeIntMap(&aggregate.ModelCallsByAlias, stats.ModelCallsByAlias)
	mergeInt64Map(&aggregate.ModelDurationMSByAlias, stats.ModelDurationMSByAlias)
	mergeIntMap(&aggregate.RateLimitsByAlias, stats.RateLimitsByAlias)
	mergeInt64Map(&aggregate.InputTokensByAlias, stats.InputTokensByAlias)
	mergeInt64Map(&aggregate.CacheCreationInputTokensByAlias, stats.CacheCreationInputTokensByAlias)
	mergeInt64Map(&aggregate.CacheReadInputTokensByAlias, stats.CacheReadInputTokensByAlias)
	mergeInt64Map(&aggregate.OutputTokensByAlias, stats.OutputTokensByAlias)
	mergeIntMap(&aggregate.TopLevelTurnsByAlias, stats.TopLevelTurnsByAlias)
	mergeIntMap(&aggregate.CallTreesByResolvedModel, stats.CallTreesByResolvedModel)
	mergeInt64Map(&aggregate.InputTokensByResolvedModel, stats.InputTokensByResolvedModel)
	mergeInt64Map(&aggregate.CacheCreationInputTokensByResolvedModel, stats.CacheCreationInputTokensByResolvedModel)
	mergeInt64Map(&aggregate.CacheReadInputTokensByResolvedModel, stats.CacheReadInputTokensByResolvedModel)
	mergeInt64Map(&aggregate.OutputTokensByResolvedModel, stats.OutputTokensByResolvedModel)
	aggregate.WorkerCalls += stats.WorkerCalls
	aggregate.ReviewerCalls += stats.ReviewerCalls
	aggregate.DecisionCommands += stats.DecisionCommands
	aggregate.FixCommands += stats.FixCommands
	aggregate.ResumeCommands += stats.ResumeCommands
	aggregate.AutoFixRounds += stats.AutoFixRounds
	aggregate.NeedsSolDecisionPackets += stats.NeedsSolDecisionPackets
	aggregate.NeedsSolReviewPackets += stats.NeedsSolReviewPackets
	aggregate.PassPackets += stats.PassPackets
	aggregate.RateLimits += stats.RateLimits
	aggregate.PacketCompactions += stats.PacketCompactions
	aggregate.SolPacketBytes += stats.SolPacketBytes
	aggregate.ProviderUnavailable += stats.ProviderUnavailable
	mergeIntMap(&aggregate.ProviderUnavailableByAlias, stats.ProviderUnavailableByAlias)
	mergeIntMap(&aggregate.RiskFloorByCategory, stats.RiskFloorByCategory)
	aggregate.SnapshotMismatches += stats.SnapshotMismatches
	mergeIntMap(&aggregate.SnapshotMismatchByAxis, stats.SnapshotMismatchByAxis)
	mergeIntMap(&aggregate.PacketRejectByCategory, stats.PacketRejectByCategory)
	mergeIntMap(&aggregate.ProbeOutcome, stats.ProbeOutcome)
	aggregate.TransientRetries += stats.TransientRetries
	mergeIntMap(&aggregate.ParentOutcomes, stats.ParentOutcomes)
	mergeIntMap(&aggregate.ParentFixOrigins, stats.ParentFixOrigins)
	mergeIntMap(&aggregate.ParentOutcomesByModel, stats.ParentOutcomesByModel)
	mergeIntMap(&aggregate.ParentOutcomesByRisk, stats.ParentOutcomesByRisk)
}

func buildStatsOutput(st *state.StateStore, all []state.TaskStats, query Query) StatsOutput {
	filtered := FilterTaskStatsForQuery(all, query.Filter)
	aggregate := NewAggregateTaskStats()
	for _, stats := range filtered {
		MergeTaskStats(&aggregate, stats)
	}
	output := statsOutputFromAggregate(st, len(filtered), aggregate, probeCallCount(aggregate.ProbeOutcome))
	output.Query = query.view(QueryPeriodBasisTask)
	output.TelemetryCoverage = statsCoverageDetail(
		st.ComputeTelemetryCoverage(filtered, query.Filter.TaskID, taskStatsIDSet(all)),
	)
	fillStatsPreflight(st, &output)
	fillStatsParentReview(st, filtered, aggregate, &output)
	output.CurrentTask = statsCurrentTaskDetail(st)
	return output
}

func taskStatsIDSet(all []state.TaskStats) map[string]bool {
	ids := make(map[string]bool, len(all))
	for _, stats := range all {
		ids[stats.TaskID] = true
	}
	return ids
}

func probeCallCount(outcomes map[string]int) int {
	total := 0
	for _, count := range outcomes {
		total += count
	}
	return total
}

func statsOutputFromAggregate(st *state.StateStore, tasks int, aggregate state.TaskStats, probeCalls int) StatsOutput {
	return StatsOutput{
		Tasks:                           tasks,
		ModelCalls:                      aggregate.ModelCalls,
		ModelCallsByAlias:               aggregate.ModelCallsByAlias,
		ProbeCalls:                      probeCalls,
		TotalAICalls:                    aggregate.ModelCalls + probeCalls,
		ModelDurationMSByAlias:          aggregate.ModelDurationMSByAlias,
		InputTokensByAlias:              aggregate.InputTokensByAlias,
		CacheCreationInputTokensByAlias: aggregate.CacheCreationInputTokensByAlias,
		CacheReadInputTokensByAlias:     aggregate.CacheReadInputTokensByAlias,
		TotalPromptTokensByAlias: SumInt64Maps(
			aggregate.InputTokensByAlias,
			aggregate.CacheCreationInputTokensByAlias,
			aggregate.CacheReadInputTokensByAlias,
		),
		OutputTokensByAlias:                     aggregate.OutputTokensByAlias,
		TopLevelTurnsByAlias:                    aggregate.TopLevelTurnsByAlias,
		CallTreesByResolvedModel:                aggregate.CallTreesByResolvedModel,
		InputTokensByResolvedModel:              aggregate.InputTokensByResolvedModel,
		CacheCreationInputTokensByResolvedModel: aggregate.CacheCreationInputTokensByResolvedModel,
		CacheReadInputTokensByResolvedModel:     aggregate.CacheReadInputTokensByResolvedModel,
		OutputTokensByResolvedModel:             aggregate.OutputTokensByResolvedModel,
		WorkerCalls:                             aggregate.WorkerCalls,
		ReviewerCalls:                           aggregate.ReviewerCalls,
		DecisionCommands:                        aggregate.DecisionCommands,
		FixCommands:                             aggregate.FixCommands,
		ResumeCommands:                          aggregate.ResumeCommands,
		TransientRetries:                        aggregate.TransientRetries,
		AutoFixRounds:                           aggregate.AutoFixRounds,
		NeedsSolDecisionPackets:                 aggregate.NeedsSolDecisionPackets,
		NeedsSolReviewPackets:                   aggregate.NeedsSolReviewPackets,
		PassPackets:                             aggregate.PassPackets,
		RateLimits:                              aggregate.RateLimits,
		RateLimitsByAlias:                       aggregate.RateLimitsByAlias,
		ProviderUnavailable:                     aggregate.ProviderUnavailable,
		ProviderUnavailableByAlias:              aggregate.ProviderUnavailableByAlias,
		PacketCompactions:                       aggregate.PacketCompactions,
		RiskFloorByCategory:                     aggregate.RiskFloorByCategory,
		SnapshotMismatches:                      aggregate.SnapshotMismatches,
		SnapshotMismatchByAxis:                  aggregate.SnapshotMismatchByAxis,
		PacketRejectByCategory:                  aggregate.PacketRejectByCategory,
		ProbeOutcome:                            aggregate.ProbeOutcome,
		ParentOutcomes:                          aggregate.ParentOutcomes,
		ParentFixOrigins:                        aggregate.ParentFixOrigins,
		ParentOutcomesByModel:                   aggregate.ParentOutcomesByModel,
		ParentOutcomesByRisk:                    aggregate.ParentOutcomesByRisk,
		SolPacketBytes:                          aggregate.SolPacketBytes,
		TelemetryDir:                            st.Path("telemetry"),
	}
}

func statsCoverageDetail(coverage state.TelemetryCoverage) statsCoverage {
	detail := statsCoverage{
		Status:        coverage.Status,
		StatsCalls:    coverage.StatsCalls,
		RawRecords:    coverage.RawRecords,
		MissingCalls:  coverage.MissingCalls,
		ExcessRecords: coverage.ExcessRecords,
		OrphanFiles:   coverage.OrphanFiles,
		UsageKnown:    coverage.UsageKnown,
		Tasks:         make([]StatsCoverageTask, 0, len(coverage.Tasks)),
	}
	for _, entry := range coverage.Tasks {
		classification := entry.Classification()
		if classification == state.CoverageComplete {
			continue
		}
		detail.Tasks = append(detail.Tasks, StatsCoverageTask{
			TaskID:         entry.TaskID,
			Classification: classification,
			StatsCalls:     entry.StatsCalls,
			RawRecords:     entry.RawRecords,
			MissingCalls:   entry.MissingCalls(),
			ExcessRecords:  entry.ExcessRecords(),
		})
	}
	return detail
}

func fillStatsPreflight(st *state.StateStore, output *StatsOutput) {
	stats, err := st.LoadPreflightStats()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			output.Preflight = statsPreflight{Status: taskview.StatusNone}
			return
		}
		output.Preflight = statsPreflight{Status: "unknown", Error: err.Error()}
		return
	}
	detail := statsPreflight{
		Status:            "ok",
		PassCount:         stats.PassCount,
		FailureCount:      stats.FailureCount,
		AvoidedModelCalls: stats.AvoidedModelCalls,
		TotalDurationMS:   stats.TotalDurationMS,
		MaxDurationMS:     stats.MaxDurationMS,
		LastDurationMS:    stats.LastDurationMS,
		LastOutcome:       stats.LastOutcome,
	}
	if !stats.LastAt.IsZero() {
		lastAt := stats.LastAt
		detail.LastAt = &lastAt
	}
	output.Preflight = detail
}

func fillStatsParentReview(st *state.StateStore, all []state.TaskStats, _ state.TaskStats, output *StatsOutput) {
	output.ParentFixRework = make([]statsParentRework, 0)
	rework := st.ComputeParentRework(all)
	origins := make([]string, 0, len(rework.ByOrigin))
	for origin := range rework.ByOrigin {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		entry := rework.ByOrigin[origin]
		output.ParentFixRework = append(output.ParentFixRework, statsParentRework{
			Origin:           origin,
			Calls:            entry.Calls,
			WorkerCalls:      entry.WorkerCalls,
			ReviewerCalls:    entry.ReviewerCalls,
			Turns:            entry.Turns,
			TreeInputTokens:  entry.TreeInputTokens,
			TreeOutputTokens: entry.TreeOutputTokens,
			WallDurationMS:   entry.WallDurationMS,
		})
	}
	output.ParentFixReworkCoverage = rework.Coverage
}

func statsCurrentTaskDetail(st *state.StateStore) statsCurrentTask {
	current := statsCurrentTask{Status: machinecli.TaskStatusPtr(st.TaskStatus())}
	if id := st.ReadOr("task.id", ""); id != "" {
		current.ID = machinecli.StringPtr(id)
		current.ArtifactDir = machinecli.StringPtr(st.ArtifactDir(id))
	}
	return current
}

func mergeIntMap(target *map[string]int, source map[string]int) {
	if *target == nil {
		*target = make(map[string]int)
	}
	for key, value := range source {
		(*target)[key] += value
	}
}

func mergeInt64Map(target *map[string]int64, source map[string]int64) {
	if *target == nil {
		*target = make(map[string]int64)
	}
	for key, value := range source {
		(*target)[key] += value
	}
}

func SumInt64Maps(values ...map[string]int64) map[string]int64 {
	result := make(map[string]int64)
	for _, items := range values {
		for key, value := range items {
			result[key] += value
		}
	}
	return result
}
