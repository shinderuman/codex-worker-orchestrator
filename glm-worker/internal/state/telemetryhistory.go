package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
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

type telemetryHistoryHeader struct {
	Version        *int            `json:"version"`
	SchemaRevision int             `json:"schema_revision"`
	TreeUsage      json.RawMessage `json:"tree_usage"`
}

type TelemetryCohortCallLogs struct {
	Version        int
	SchemaRevision int
	Logs           []TaskCallLogs
}

const (
	TelemetryScopeCurrent = "current"
	TelemetryScopeHistory = "history"

	TelemetryExclusionCurrentSchema = "current-schema"
	TelemetryExclusionNewerSchema   = "newer-schema"

	telemetryMalformedReasonDecode = "line-json-decode"
	telemetryMalformedReasonHeader = "missing-version"

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
	dir := s.Path("telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("telemetry dirを読めません: %w", err)
	}

	scan := &TelemetryHistoryScan{Dir: dir, Status: telemetryHistoryStatusOK, Cohorts: []TelemetryCohortScan{}}
	cohorts := make(map[telemetryCohortKey]*telemetryCohortAccumulator)
	cohortTaskLogs := make(map[telemetryCohortKey]map[string][]ModelCallLog)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !filter.MatchesTask(taskID) {
			continue
		}
		scan.FilesConsidered++
		if !ValidGeneratedUUID(taskID) {
			scan.IgnoredFiles = append(scan.IgnoredFiles, name)
			continue
		}
		if err := s.absorbTelemetryHistoryFile(scan, cohorts, cohortTaskLogs, name, taskID, filter); err != nil {
			scan.Status = telemetryHistoryStatusPartial
			scan.UnreadableFiles = append(scan.UnreadableFiles, TelemetryFileError{
				File:  name,
				Error: err.Error(),
			})
		}
	}
	if scan.FilesConsidered == 0 {
		scan.Status = telemetryHistoryStatusNone
	}
	scan.Cohorts = collectTelemetryCohortScans(cohorts)
	scan.historyCohortLogs = collectTelemetryCohortLogs(cohortTaskLogs)
	return scan, nil
}

func (s *StateStore) absorbTelemetryHistoryFile(
	scan *TelemetryHistoryScan,
	cohorts map[telemetryCohortKey]*telemetryCohortAccumulator,
	cohortTaskLogs map[telemetryCohortKey]map[string][]ModelCallLog,
	fileName string,
	taskID string,
	filter TelemetryQueryFilter,
) error {
	file, err := os.Open(s.ModelCallLogPath(taskID))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		scan.absorbTelemetryHistoryLine(cohorts, cohortTaskLogs, fileName, taskID, filter, scanner.Bytes())
	}
	return scanner.Err()
}

func (s *TelemetryHistoryScan) absorbTelemetryHistoryLine(
	cohorts map[telemetryCohortKey]*telemetryCohortAccumulator,
	cohortTaskLogs map[telemetryCohortKey]map[string][]ModelCallLog,
	fileName string,
	taskID string,
	filter TelemetryQueryFilter,
	line []byte,
) {
	if len(line) == 0 {
		return
	}
	var header telemetryHistoryHeader
	if err := json.Unmarshal(line, &header); err != nil {
		s.countTelemetryMalformed(telemetryMalformedReasonDecode)
		return
	}
	if header.Version == nil {
		s.countTelemetryMalformed(telemetryMalformedReasonHeader)
		return
	}
	var record ModelCallLog
	if err := json.Unmarshal(line, (*modelCallLogAlias)(&record)); err != nil {
		s.countTelemetryMalformed(telemetryMalformedReasonDecode)
		return
	}
	if filter.ExcludesUndated(record.StartedAt) {
		s.RecordsUndatedExcluded++
		return
	}
	if !filter.CoversTime(record.StartedAt) {
		s.RecordsOutsidePeriod++
		return
	}

	key := telemetryCohortKey{version: *header.Version, schemaRevision: header.SchemaRevision}
	cohort := cohorts[key]
	if cohort == nil {
		cohort = newTelemetryCohortAccumulator(key)
		cohorts[key] = cohort
	}
	usagePresent := len(header.TreeUsage) > 0 && string(header.TreeUsage) != "null"
	cohort.absorb(fileName, taskID, record, usagePresent)
	if cohort.scan.ExcludedReason == "" {
		if cohortTaskLogs[key] == nil {
			cohortTaskLogs[key] = make(map[string][]ModelCallLog)
		}
		cohortTaskLogs[key][taskID] = append(cohortTaskLogs[key][taskID], record)
	}
}

func (s *TelemetryHistoryScan) countTelemetryMalformed(reason string) {
	s.Malformed.Count++
	if s.Malformed.ByReason == nil {
		s.Malformed.ByReason = make(map[string]int)
	}
	s.Malformed.ByReason[reason]++
}

func newTelemetryCohortAccumulator(key telemetryCohortKey) *telemetryCohortAccumulator {
	cohort := &telemetryCohortAccumulator{
		scan: TelemetryCohortScan{
			Version:        key.version,
			SchemaRevision: key.schemaRevision,
			CurrentSchema:  key.version == ModelCallLogVersion && key.schemaRevision == modelCallLogSchemaRevision,
			FileNames:      []string{},
		},
		fileNames: make(map[string]bool),
		taskIDs:   make(map[string]bool),
	}
	cohort.aggregates = newTelemetryCohortAggregates()
	switch {
	case cohort.scan.CurrentSchema:
		cohort.scan.ExcludedReason = TelemetryExclusionCurrentSchema
	case key.version > ModelCallLogVersion ||
		(key.version == ModelCallLogVersion && key.schemaRevision > modelCallLogSchemaRevision):
		cohort.scan.ExcludedReason = TelemetryExclusionNewerSchema
	}
	return cohort
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
	if a.scan.ExcludedReason != "" {
		return
	}
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
		if cohort.scan.ExcludedReason == "" {
			cohort.scan.Aggregates = &cohort.aggregates
		}
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
