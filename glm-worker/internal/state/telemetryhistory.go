package state

import (
	"slices"
	"time"
)

type TelemetryQueryFilter struct {
	TaskID string
	Since  time.Time
	Until  time.Time
}

type TelemetryFileError struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

type TelemetryMalformedRecords struct {
	Count    int            `json:"count"`
	ByReason map[string]int `json:"by_reason,omitempty"`
}

type TelemetryCohortCoverage struct {
	TaskCallsWithTurns    int  `json:"task_calls_with_turns"`
	TaskCallsWithDuration int  `json:"task_calls_with_duration"`
	TaskCallsWithUsage    int  `json:"task_calls_with_usage"`
	TaskCallsMissingUsage int  `json:"task_calls_missing_usage"`
	UsageTotalsKnown      bool `json:"usage_totals_known"`
}

type TelemetryCohortAggregates struct {
	ModelCalls                           int              `json:"model_calls"`
	ModelCallsByAlias                    map[string]int   `json:"model_calls_by_alias"`
	ModelDurationMSByAlias               map[string]int64 `json:"model_duration_ms_by_alias"`
	TopLevelTurnsByAlias                 map[string]int   `json:"top_level_turns_by_alias"`
	InputTokensByAlias                   map[string]int64 `json:"input_tokens_by_alias"`
	CacheCreationInputTokensByAlias      map[string]int64 `json:"cache_creation_input_tokens_by_alias"`
	CacheReadInputTokensByAlias          map[string]int64 `json:"cache_read_input_tokens_by_alias"`
	OutputTokensByAlias                  map[string]int64 `json:"output_tokens_by_alias"`
	CallTreesByResolvedModel             map[string]int   `json:"call_trees_by_resolved_model"`
	InputTokensByResolvedModel           map[string]int64 `json:"input_tokens_by_resolved_model"`
	CacheCreationInputTokensByResolvedMo map[string]int64 `json:"cache_creation_input_tokens_by_resolved_model"`
	CacheReadInputTokensByResolvedModel  map[string]int64 `json:"cache_read_input_tokens_by_resolved_model"`
	OutputTokensByResolvedModel          map[string]int64 `json:"output_tokens_by_resolved_model"`
}

type TelemetryCohortScan struct {
	Version        int                        `json:"version"`
	SchemaRevision int                        `json:"schema_revision"`
	CurrentSchema  bool                       `json:"current_schema"`
	ExcludedReason string                     `json:"excluded_reason,omitempty"`
	Files          int                        `json:"files"`
	FileNames      []string                   `json:"file_names"`
	Tasks          int                        `json:"tasks"`
	Records        CallRecordCounts           `json:"records"`
	FirstStartedAt *time.Time                 `json:"first_started_at,omitempty"`
	LastStartedAt  *time.Time                 `json:"last_started_at,omitempty"`
	Coverage       TelemetryCohortCoverage    `json:"coverage"`
	Aggregates     *TelemetryCohortAggregates `json:"aggregates,omitempty"`
}

type TelemetryHistoryScan struct {
	Dir                    string                    `json:"dir"`
	Status                 string                    `json:"status"`
	FilesConsidered        int                       `json:"files_considered"`
	IgnoredFiles           []string                  `json:"ignored_files,omitempty"`
	UnreadableFiles        []TelemetryFileError      `json:"unreadable_files,omitempty"`
	RecordsOutsidePeriod   int                       `json:"records_outside_period,omitempty"`
	RecordsUndatedExcluded int                       `json:"records_undated_excluded,omitempty"`
	Malformed              TelemetryMalformedRecords `json:"malformed_records"`
	Cohorts                []TelemetryCohortScan     `json:"cohorts"`

	historyCohortLogs []TelemetryCohortCallLogs
}

type telemetryCohortKey struct {
	version        int
	schemaRevision int
}

type telemetryCohortAccumulator struct {
	scan       TelemetryCohortScan
	fileNames  map[string]bool
	taskIDs    map[string]bool
	aggregates TelemetryCohortAggregates
}

type TelemetryCohortCallLogs struct {
	Version        int
	SchemaRevision int
	Logs           []TaskCallLogs
}

const (
	TelemetryScopeCurrent = "current"
	TelemetryScopeHistory = "history"

	telemetryMalformedReasonDecode            = "line-json-decode"
	telemetryMalformedReasonHeader            = "missing-version"
	telemetryMalformedReasonUnsupportedSchema = "unsupported-schema"

	telemetryHistoryStatusOK      = "ok"
	telemetryHistoryStatusPartial = "partial"
	telemetryHistoryStatusNone    = "none"
)

func (s *TelemetryHistoryScan) HistoryCohortLogs() []TelemetryCohortCallLogs {
	return s.historyCohortLogs
}

func (f TelemetryQueryFilter) HasPeriod() bool {
	return !f.Since.IsZero() || !f.Until.IsZero()
}

func (f TelemetryQueryFilter) MatchesTask(taskID string) bool {
	return f.TaskID == "" || f.TaskID == taskID
}

func (f TelemetryQueryFilter) ExcludesUndated(at time.Time) bool {
	return at.IsZero() && f.HasPeriod()
}

func (f TelemetryQueryFilter) CoversTime(at time.Time) bool {
	if at.IsZero() {
		return !f.HasPeriod()
	}
	if !f.Since.IsZero() && at.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && !at.Before(f.Until) {
		return false
	}
	return true
}

func (s *StateStore) ScanTelemetryHistory(filter TelemetryQueryFilter) (*TelemetryHistoryScan, error) {
	corpus, err := s.scanTelemetryCorpus(filter)
	if err != nil {
		return nil, err
	}

	scan := &TelemetryHistoryScan{
		Dir:                    corpus.dir,
		Status:                 telemetryHistoryStatusOK,
		FilesConsidered:        corpus.filesConsidered,
		IgnoredFiles:           corpus.ignoredFiles,
		UnreadableFiles:        corpus.unreadableFiles,
		RecordsOutsidePeriod:   corpus.recordsOutsidePeriod,
		RecordsUndatedExcluded: corpus.recordsUndatedExcluded,
		Cohorts:                []TelemetryCohortScan{},
	}
	if len(scan.UnreadableFiles) > 0 {
		scan.Status = telemetryHistoryStatusPartial
	}
	if scan.FilesConsidered == 0 {
		scan.Status = telemetryHistoryStatusNone
	}

	cohorts, cohortTaskLogs := scan.absorbTelemetryCorpus(corpus)
	scan.Cohorts = collectTelemetryCohortScans(cohorts)
	scan.historyCohortLogs = collectTelemetryCohortLogs(cohortTaskLogs)
	return scan, nil
}

func (s *TelemetryHistoryScan) absorbTelemetryCorpus(corpus *telemetryCorpusScan) (map[telemetryCohortKey]*telemetryCohortAccumulator, map[telemetryCohortKey]map[string][]ModelCallLog) {
	cohorts := make(map[telemetryCohortKey]*telemetryCohortAccumulator)
	cohortTaskLogs := make(map[telemetryCohortKey]map[string][]ModelCallLog)
	for _, file := range corpus.files {
		for _, record := range file.records {
			s.absorbTelemetryCorpusRecord(cohorts, cohortTaskLogs, file, record)
		}
	}
	return cohorts, cohortTaskLogs
}

func (s *TelemetryHistoryScan) absorbTelemetryCorpusRecord(
	cohorts map[telemetryCohortKey]*telemetryCohortAccumulator,
	cohortTaskLogs map[telemetryCohortKey]map[string][]ModelCallLog,
	file telemetryCorpusFile,
	record telemetryCorpusRecord,
) {
	if record.malformedReason != "" {
		s.countTelemetryMalformed(record.malformedReason)
		return
	}
	if !record.current {
		return
	}

	key := telemetryCohortKey{version: ModelCallLogVersion, schemaRevision: ModelCallLogSchemaRevision}
	cohort := cohorts[key]
	if cohort == nil {
		cohort = newTelemetryCohortAccumulator(key)
		cohorts[key] = cohort
	}
	cohort.absorb(file.name, file.taskID, record.log, record.usagePresent)
	if cohortTaskLogs[key] == nil {
		cohortTaskLogs[key] = make(map[string][]ModelCallLog)
	}
	cohortTaskLogs[key][file.taskID] = append(cohortTaskLogs[key][file.taskID], record.log)
}

func (s *TelemetryHistoryScan) countTelemetryMalformed(reason string) {
	s.Malformed.Count++
	if s.Malformed.ByReason == nil {
		s.Malformed.ByReason = make(map[string]int)
	}
	s.Malformed.ByReason[reason]++
}

func newTelemetryCohortAccumulator(key telemetryCohortKey) *telemetryCohortAccumulator {
	return &telemetryCohortAccumulator{
		scan: TelemetryCohortScan{
			Version:        key.version,
			SchemaRevision: key.schemaRevision,
			CurrentSchema:  true,
			FileNames:      []string{},
		},
		fileNames:  make(map[string]bool),
		taskIDs:    make(map[string]bool),
		aggregates: newTelemetryCohortAggregates(),
	}
}

func newTelemetryCohortAggregates() TelemetryCohortAggregates {
	return TelemetryCohortAggregates{
		ModelCallsByAlias:                    map[string]int{},
		ModelDurationMSByAlias:               map[string]int64{},
		TopLevelTurnsByAlias:                 map[string]int{},
		InputTokensByAlias:                   map[string]int64{},
		CacheCreationInputTokensByAlias:      map[string]int64{},
		CacheReadInputTokensByAlias:          map[string]int64{},
		OutputTokensByAlias:                  map[string]int64{},
		CallTreesByResolvedModel:             map[string]int{},
		InputTokensByResolvedModel:           map[string]int64{},
		CacheCreationInputTokensByResolvedMo: map[string]int64{},
		CacheReadInputTokensByResolvedModel:  map[string]int64{},
		OutputTokensByResolvedModel:          map[string]int64{},
	}
}

func (a *telemetryCohortAccumulator) absorb(fileName string, taskID string, record ModelCallLog, usagePresent bool) {
	a.fileNames[fileName] = true
	recordTask := record.TaskID
	if recordTask == "" {
		recordTask = taskID
	}
	a.taskIDs[recordTask] = true
	a.scan.Records.Read++
	switch record.CallType {
	case CallTypeTask:
		a.scan.Records.Task++
	case CallTypeEvent:
		a.scan.Records.Event++
	case CallTypeProbe:
		a.scan.Records.Probe++
	default:
		a.scan.Records.Other++
	}
	a.absorbPeriod(record)
	if record.CallType != CallTypeTask {
		return
	}
	a.absorbCoverage(record, usagePresent)
	a.absorbAggregates(record, usagePresent)
}

func (a *telemetryCohortAccumulator) absorbPeriod(record ModelCallLog) {
	if record.StartedAt.IsZero() {
		return
	}
	if a.scan.FirstStartedAt == nil || record.StartedAt.Before(*a.scan.FirstStartedAt) {
		startedAt := record.StartedAt
		a.scan.FirstStartedAt = &startedAt
	}
	if a.scan.LastStartedAt == nil || record.StartedAt.After(*a.scan.LastStartedAt) {
		startedAt := record.StartedAt
		a.scan.LastStartedAt = &startedAt
	}
}

func (a *telemetryCohortAccumulator) absorbCoverage(record ModelCallLog, usagePresent bool) {
	if record.TopLevelTurns > 0 {
		a.scan.Coverage.TaskCallsWithTurns++
	}
	if record.WallDurationMS > 0 {
		a.scan.Coverage.TaskCallsWithDuration++
	}
	if usagePresent {
		a.scan.Coverage.TaskCallsWithUsage++
	} else {
		a.scan.Coverage.TaskCallsMissingUsage++
	}
}

func (a *telemetryCohortAccumulator) absorbAggregates(record ModelCallLog, usagePresent bool) {
	a.aggregates.ModelCalls++
	addInt(&a.aggregates.ModelCallsByAlias, record.ModelAlias, 1)
	addInt64(&a.aggregates.ModelDurationMSByAlias, record.ModelAlias, record.WallDurationMS)
	addInt(&a.aggregates.TopLevelTurnsByAlias, record.ModelAlias, record.TopLevelTurns)
	if usagePresent {
		addInt64(&a.aggregates.InputTokensByAlias, record.ModelAlias, record.TreeUsage.InputTokens)
		addInt64(&a.aggregates.CacheCreationInputTokensByAlias, record.ModelAlias, record.TreeUsage.CacheCreationInputTokens)
		addInt64(&a.aggregates.CacheReadInputTokensByAlias, record.ModelAlias, record.TreeUsage.CacheReadInputTokens)
		addInt64(&a.aggregates.OutputTokensByAlias, record.ModelAlias, record.TreeUsage.OutputTokens)
	}
	for model, usage := range record.ResolvedModelUsage {
		addInt(&a.aggregates.CallTreesByResolvedModel, model, 1)
		addInt64(&a.aggregates.InputTokensByResolvedModel, model, usage.InputTokens)
		addInt64(&a.aggregates.CacheCreationInputTokensByResolvedMo, model, usage.CacheCreationInputTokens)
		addInt64(&a.aggregates.CacheReadInputTokensByResolvedModel, model, usage.CacheReadInputTokens)
		addInt64(&a.aggregates.OutputTokensByResolvedModel, model, usage.OutputTokens)
	}
}

func compareTelemetryCohortKeys(a telemetryCohortKey, b telemetryCohortKey) int {
	if a.version != b.version {
		return a.version - b.version
	}
	return a.schemaRevision - b.schemaRevision
}

func sortedTelemetryCohortKeys[V any](values map[telemetryCohortKey]V) []telemetryCohortKey {
	keys := make([]telemetryCohortKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareTelemetryCohortKeys)
	return keys
}

func collectTelemetryCohortScans(cohorts map[telemetryCohortKey]*telemetryCohortAccumulator) []TelemetryCohortScan {
	keys := sortedTelemetryCohortKeys(cohorts)

	result := make([]TelemetryCohortScan, 0, len(keys))
	for _, key := range keys {
		cohort := cohorts[key]
		cohort.scan.Files = len(cohort.fileNames)
		cohort.scan.FileNames = sortedKeys(cohort.fileNames)
		cohort.scan.Tasks = len(cohort.taskIDs)
		cohort.scan.Coverage.UsageTotalsKnown = cohort.scan.Coverage.TaskCallsMissingUsage == 0
		cohort.scan.Aggregates = &cohort.aggregates
		result = append(result, cohort.scan)
	}
	return result
}

func collectTelemetryCohortLogs(cohortTaskLogs map[telemetryCohortKey]map[string][]ModelCallLog) []TelemetryCohortCallLogs {
	keys := sortedTelemetryCohortKeys(cohortTaskLogs)

	logs := make([]TelemetryCohortCallLogs, 0, len(keys))
	for _, key := range keys {
		taskLogs := cohortTaskLogs[key]
		taskIDs := make([]string, 0, len(taskLogs))
		for taskID := range taskLogs {
			taskIDs = append(taskIDs, taskID)
		}
		slices.Sort(taskIDs)

		cohortLogs := make([]TaskCallLogs, 0, len(taskIDs))
		for _, taskID := range taskIDs {
			cohortLogs = append(cohortLogs, TaskCallLogs{TaskID: taskID, Logs: taskLogs[taskID]})
		}
		logs = append(logs, TelemetryCohortCallLogs{
			Version:        key.version,
			SchemaRevision: key.schemaRevision,
			Logs:           cohortLogs,
		})
	}
	return logs
}
