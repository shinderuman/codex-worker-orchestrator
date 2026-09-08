package app

import (
	"io"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type telemetryCompactSummary struct {
	Version     int                         `json:"version"`
	Query       telemetryCompactQuery       `json:"query"`
	Scan        telemetryCompactScan        `json:"scan"`
	Cohorts     []telemetryCompactCohort    `json:"cohorts"`
	Stats       telemetryCompactStats       `json:"stats"`
	ParentUsage telemetryCompactParentUsage `json:"parent_usage"`
	Bounds      telemetryCompactBounds      `json:"bounds"`
}

type telemetryCompactQuery struct {
	Scope                string  `json:"scope"`
	TaskID               string  `json:"task_id,omitempty"`
	Since                *string `json:"since,omitempty"`
	Until                *string `json:"until,omitempty"`
	TelemetryPeriodBasis string  `json:"telemetry_period_basis"`
	StatsPeriodBasis     string  `json:"stats_period_basis"`
}

type telemetryCompactScan struct {
	Status                 string         `json:"status"`
	Dir                    string         `json:"dir"`
	FilesConsidered        int            `json:"files_considered"`
	RecordsOutsidePeriod   int            `json:"records_outside_period,omitempty"`
	RecordsUndatedExcluded int            `json:"records_undated_excluded,omitempty"`
	IgnoredFiles           int            `json:"ignored_files"`
	UnreadableFiles        int            `json:"unreadable_files"`
	MalformedRecords       int            `json:"malformed_records"`
	MalformedByReason      map[string]int `json:"malformed_by_reason,omitempty"`
}

type telemetryCompactCohort struct {
	Version        int                           `json:"version"`
	SchemaRevision int                           `json:"schema_revision"`
	CurrentSchema  bool                          `json:"current_schema"`
	ExcludedReason string                        `json:"excluded_reason,omitempty"`
	Files          int                           `json:"files"`
	Tasks          int                           `json:"tasks"`
	Records        state.CallRecordCounts        `json:"records"`
	FirstStartedAt *time.Time                    `json:"first_started_at,omitempty"`
	LastStartedAt  *time.Time                    `json:"last_started_at,omitempty"`
	Coverage       state.TelemetryCohortCoverage `json:"coverage"`
	Outliers       telemetryCompactOutliers      `json:"outliers"`
}

type telemetryCompactOutliers struct {
	Status         string                        `json:"status"`
	EligibleGroups int                           `json:"eligible_groups"`
	OutlierCalls   int                           `json:"outlier_calls"`
	OutlierTasks   int                           `json:"outlier_tasks"`
	TopCalls       []telemetryCompactOutlierCall `json:"top_calls"`
	TopTasks       []telemetryCompactOutlierTask `json:"top_tasks"`
	TruncatedCalls int                           `json:"truncated_calls"`
	TruncatedTasks int                           `json:"truncated_tasks"`
}

type telemetryCompactOutlierCall struct {
	TaskID     string `json:"task_id"`
	CallID     string `json:"call_id"`
	Phase      string `json:"phase"`
	Turns      int64  `json:"turns"`
	DurationMS int64  `json:"duration_ms"`
}

type telemetryCompactOutlierTask struct {
	TaskID          string `json:"task_id"`
	Calls           int    `json:"calls"`
	TurnsTotal      int64  `json:"turns_total"`
	DurationMSTotal int64  `json:"duration_ms_total"`
}

type telemetryCompactStats struct {
	PeriodBasis                      string           `json:"period_basis"`
	CohortVersion                    int              `json:"cohort_version"`
	CohortSchemaRevision             int              `json:"cohort_schema_revision"`
	TasksConsidered                  int              `json:"tasks_considered"`
	CurrentSchemaTasks               int              `json:"current_schema_tasks"`
	TasksWithoutCurrentSchemaRecords int              `json:"tasks_without_current_schema_records"`
	WorkerCalls                      int              `json:"worker_calls"`
	ReviewerCalls                    int              `json:"reviewer_calls"`
	FixCommands                      int              `json:"fix_commands"`
	ResumeCommands                   int              `json:"resume_commands"`
	RateLimits                       int              `json:"rate_limits"`
	ModelCallsByAlias                map[string]int   `json:"model_calls_by_alias"`
	TotalPromptTokensByAlias         map[string]int64 `json:"total_prompt_tokens_by_alias"`
	OutputTokensByAlias              map[string]int64 `json:"output_tokens_by_alias"`
	TurnsByAlias                     map[string]int   `json:"turns_by_alias"`
	ParentOutcomes                   map[string]int   `json:"parent_outcomes"`
	ParentFixOrigins                 map[string]int   `json:"parent_fix_origins"`
	SolPacketBytes                   int              `json:"sol_packet_bytes"`
}

type telemetryCompactParentUsage struct {
	Tasks     int            `json:"tasks"`
	Available int            `json:"available"`
	Ambiguous int            `json:"ambiguous"`
	Unknown   int            `json:"unknown"`
	ByStatus  map[string]int `json:"by_status"`
}

type telemetryCompactBounds struct {
	TopOutlierCalls int `json:"top_outlier_calls"`
	TopOutlierTasks int `json:"top_outlier_tasks"`
}

type telemetryCompactCohortKey struct {
	version        int
	schemaRevision int
}

const telemetryCompactSummaryVersion = 1

const telemetryCompactTopOutlierCalls = 10

const telemetryCompactTopOutlierTasks = 10

const telemetryCompactOutliersEvaluated = "evaluated"

const telemetryCompactOutliersNotEvaluated = "not-evaluated"

const telemetryCompactTaskFileSuffix = ".jsonl"

func printTelemetryCompactSummary(cfg config.AppConfig, st *state.StateStore, query TelemetryQueryArgs, stdout io.Writer) error {
	summary, err := buildTelemetryCompactSummary(cfg, st, query)
	if err != nil {
		return err
	}
	return writeJSON(stdout, summary)
}

func buildTelemetryCompactSummary(cfg config.AppConfig, st *state.StateStore, query TelemetryQueryArgs) (telemetryCompactSummary, error) {
	historyScan, err := st.ScanTelemetryHistory(query.Filter)
	if err != nil {
		return telemetryCompactSummary{}, err
	}
	statsTasks, err := st.AllTaskStats()
	if err != nil {
		return telemetryCompactSummary{}, err
	}
	filteredStats := filterTaskStatsForQuery(statsTasks, query.Filter)
	cohorts, scan, err := telemetryCompactCohortsAndScan(st, query, historyScan)
	if err != nil {
		return telemetryCompactSummary{}, err
	}
	return telemetryCompactSummary{
		Version:     telemetryCompactSummaryVersion,
		Query:       telemetryCompactQueryView(query),
		Scan:        scan,
		Cohorts:     cohorts,
		Stats:       buildTelemetryCompactStats(filteredStats, telemetryCompactCurrentSchemaTasks(historyScan)),
		ParentUsage: buildTelemetryCompactParentUsage(cfg, st, filteredStats),
		Bounds: telemetryCompactBounds{
			TopOutlierCalls: telemetryCompactTopOutlierCalls,
			TopOutlierTasks: telemetryCompactTopOutlierTasks,
		},
	}, nil
}

func telemetryCompactCohortsAndScan(st *state.StateStore, query TelemetryQueryArgs, historyScan *state.TelemetryHistoryScan) ([]telemetryCompactCohort, telemetryCompactScan, error) {
	if query.isHistory() {
		return telemetryCompactHistoryCohorts(historyScan), telemetryCompactHistoryScanView(historyScan), nil
	}
	currentScan, err := scanTelemetryTaskLogs(st, query.Filter)
	if err != nil {
		return nil, telemetryCompactScan{}, err
	}
	return telemetryCompactCurrentCohorts(historyScan, currentScan), telemetryCompactCurrentScanView(currentScan), nil
}

func telemetryCompactHistoryCohorts(historyScan *state.TelemetryHistoryScan) []telemetryCompactCohort {
	reports := make(map[telemetryCompactCohortKey]state.CallOutlierReport)
	for _, cohortLogs := range historyScan.HistoryCohortLogs() {
		key := telemetryCompactCohortKey{version: cohortLogs.Version, schemaRevision: cohortLogs.SchemaRevision}
		reports[key] = state.BuildCallOutlierReport(cohortLogs.Logs)
	}
	cohorts := make([]telemetryCompactCohort, 0, len(historyScan.Cohorts))
	for _, cohort := range historyScan.Cohorts {
		key := telemetryCompactCohortKey{version: cohort.Version, schemaRevision: cohort.SchemaRevision}
		report, evaluated := reports[key]
		cohorts = append(cohorts, telemetryCompactCohort{
			Version:        cohort.Version,
			SchemaRevision: cohort.SchemaRevision,
			CurrentSchema:  cohort.CurrentSchema,
			ExcludedReason: cohort.ExcludedReason,
			Files:          cohort.Files,
			Tasks:          cohort.Tasks,
			Records:        cohort.Records,
			FirstStartedAt: cohort.FirstStartedAt,
			LastStartedAt:  cohort.LastStartedAt,
			Coverage:       cohort.Coverage,
			Outliers:       telemetryCompactOutliersSection(report, evaluated),
		})
	}
	return cohorts
}

func telemetryCompactCurrentCohorts(historyScan *state.TelemetryHistoryScan, currentScan *telemetryScan) []telemetryCompactCohort {
	report := state.BuildCallOutlierReport(currentScan.logs)
	cohorts := make([]telemetryCompactCohort, 0, 1)
	for _, cohort := range historyScan.Cohorts {
		if !cohort.CurrentSchema {
			continue
		}
		cohorts = append(cohorts, telemetryCompactCohort{
			Version:        cohort.Version,
			SchemaRevision: cohort.SchemaRevision,
			CurrentSchema:  cohort.CurrentSchema,
			Files:          cohort.Files,
			Tasks:          cohort.Tasks,
			Records:        cohort.Records,
			FirstStartedAt: cohort.FirstStartedAt,
			LastStartedAt:  cohort.LastStartedAt,
			Coverage:       cohort.Coverage,
			Outliers:       telemetryCompactOutliersSection(report, true),
		})
	}
	if len(cohorts) == 0 {
		cohorts = append(cohorts, telemetryCompactCurrentSchemaBoundaryCohort(report))
	}
	return cohorts
}

func telemetryCompactCurrentSchemaBoundaryCohort(report state.CallOutlierReport) telemetryCompactCohort {
	return telemetryCompactCohort{
		Version:        state.ModelCallLogVersion,
		SchemaRevision: state.ModelCallLogSchemaRevision,
		CurrentSchema:  true,
		Records:        state.CallRecordCounts{},
		Coverage:       state.TelemetryCohortCoverage{UsageTotalsKnown: true},
		Outliers:       telemetryCompactOutliersSection(report, true),
	}
}

func telemetryCompactOutliersSection(report state.CallOutlierReport, evaluated bool) telemetryCompactOutliers {
	section := telemetryCompactOutliers{
		Status:   telemetryCompactOutliersNotEvaluated,
		TopCalls: []telemetryCompactOutlierCall{},
		TopTasks: []telemetryCompactOutlierTask{},
	}
	if !evaluated {
		return section
	}
	section.Status = telemetryCompactOutliersEvaluated
	for _, distribution := range report.Distributions {
		if distribution.OutlierEligible {
			section.EligibleGroups++
		}
	}
	section.OutlierCalls = len(report.OutlierCalls)
	section.OutlierTasks = len(report.OutlierTasks)
	for index, call := range report.OutlierCalls {
		if index >= telemetryCompactTopOutlierCalls {
			break
		}
		section.TopCalls = append(section.TopCalls, telemetryCompactOutlierCall{
			TaskID: call.TaskID, CallID: call.CallID, Phase: call.Phase,
			Turns: call.Turns, DurationMS: call.DurationMS,
		})
	}
	for index, task := range report.OutlierTasks {
		if index >= telemetryCompactTopOutlierTasks {
			break
		}
		section.TopTasks = append(section.TopTasks, telemetryCompactOutlierTask{
			TaskID: task.TaskID, Calls: task.Calls,
			TurnsTotal: task.TurnsTotal, DurationMSTotal: task.DurationMSTotal,
		})
	}
	section.TruncatedCalls = section.OutlierCalls - len(section.TopCalls)
	section.TruncatedTasks = section.OutlierTasks - len(section.TopTasks)
	return section
}

func telemetryCompactHistoryScanView(scan *state.TelemetryHistoryScan) telemetryCompactScan {
	return telemetryCompactScan{
		Status:                 scan.Status,
		Dir:                    scan.Dir,
		FilesConsidered:        scan.FilesConsidered,
		RecordsOutsidePeriod:   scan.RecordsOutsidePeriod,
		RecordsUndatedExcluded: scan.RecordsUndatedExcluded,
		IgnoredFiles:           len(scan.IgnoredFiles),
		UnreadableFiles:        len(scan.UnreadableFiles),
		MalformedRecords:       scan.Malformed.Count,
		MalformedByReason:      scan.Malformed.ByReason,
	}
}

func telemetryCompactCurrentScanView(scan *telemetryScan) telemetryCompactScan {
	return telemetryCompactScan{
		Status:                 scan.Status,
		Dir:                    scan.Dir,
		FilesConsidered:        scan.Files + len(scan.IgnoredFiles) + len(scan.UnreadableTasks),
		RecordsOutsidePeriod:   scan.RecordsOutsidePeriod,
		RecordsUndatedExcluded: scan.RecordsUndatedExcluded,
		IgnoredFiles:           len(scan.IgnoredFiles),
		UnreadableFiles:        len(scan.UnreadableTasks),
	}
}

func telemetryCompactQueryView(query TelemetryQueryArgs) telemetryCompactQuery {
	view := telemetryCompactQuery{
		Scope:                query.resolvedScope(),
		TaskID:               query.Filter.TaskID,
		TelemetryPeriodBasis: telemetryQueryPeriodBasisRecord,
		StatsPeriodBasis:     telemetryQueryPeriodBasisTask,
	}
	if !query.Filter.Since.IsZero() {
		since := query.Filter.Since.UTC().Format(time.RFC3339Nano)
		view.Since = &since
	}
	if !query.Filter.Until.IsZero() {
		until := query.Filter.Until.UTC().Format(time.RFC3339Nano)
		view.Until = &until
	}
	return view
}

func telemetryCompactCurrentSchemaTasks(scan *state.TelemetryHistoryScan) map[string]bool {
	tasks := make(map[string]bool)
	for _, cohort := range scan.Cohorts {
		if !cohort.CurrentSchema {
			continue
		}
		for _, name := range cohort.FileNames {
			tasks[strings.TrimSuffix(name, telemetryCompactTaskFileSuffix)] = true
		}
	}
	return tasks
}

func buildTelemetryCompactStats(filtered []state.TaskStats, membership map[string]bool) telemetryCompactStats {
	aggregate := newAggregateTaskStats()
	currentSchemaTasks := 0
	for _, stats := range filtered {
		if !membership[stats.TaskID] {
			continue
		}
		currentSchemaTasks++
		mergeTaskStats(&aggregate, stats)
	}
	return telemetryCompactStats{
		PeriodBasis:                      telemetryQueryPeriodBasisTask,
		CohortVersion:                    state.ModelCallLogVersion,
		CohortSchemaRevision:             state.ModelCallLogSchemaRevision,
		TasksConsidered:                  len(filtered),
		CurrentSchemaTasks:               currentSchemaTasks,
		TasksWithoutCurrentSchemaRecords: len(filtered) - currentSchemaTasks,
		WorkerCalls:                      aggregate.WorkerCalls,
		ReviewerCalls:                    aggregate.ReviewerCalls,
		FixCommands:                      aggregate.FixCommands,
		ResumeCommands:                   aggregate.ResumeCommands,
		RateLimits:                       aggregate.RateLimits,
		ModelCallsByAlias:                aggregate.ModelCallsByAlias,
		TotalPromptTokensByAlias:         sumInt64Maps(aggregate.InputTokensByAlias, aggregate.CacheCreationInputTokensByAlias, aggregate.CacheReadInputTokensByAlias),
		OutputTokensByAlias:              aggregate.OutputTokensByAlias,
		TurnsByAlias:                     aggregate.TopLevelTurnsByAlias,
		ParentOutcomes:                   aggregate.ParentOutcomes,
		ParentFixOrigins:                 aggregate.ParentFixOrigins,
		SolPacketBytes:                   aggregate.SolPacketBytes,
	}
}

func buildTelemetryCompactParentUsage(cfg config.AppConfig, st *state.StateStore, filtered []state.TaskStats) telemetryCompactParentUsage {
	return buildTelemetryCompactParentUsageWithScans(cfg, st, filtered, scanCodexRollouts, scanCodexRolloutChainWindow)
}

func buildTelemetryCompactParentUsageWithScans(cfg config.AppConfig, st *state.StateStore, filtered []state.TaskStats, enumerate func(string) ([]codexRollout, error), scanChain func([]codexRollout, time.Time, time.Time) (bundleRolloutScan, error)) telemetryCompactParentUsage {
	usage := telemetryCompactParentUsage{ByStatus: make(map[string]int)}
	batch := newParentUsageBatch(cfg.CodexConfigDir, filtered, enumerate, scanChain)
	for _, stats := range filtered {
		task := bundleTask{ID: stats.TaskID, Status: string(stats.Status), Stats: stats}
		evidence := batch.evidence(task)
		report := buildParentUsageReportFromScan(st, task, evidence.association, evidence.scan, evidence.err)
		usage.ByStatus[report.ParentSession.Status+"/"+report.Intervals.TaskExecution.Tokens.Status]++
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
