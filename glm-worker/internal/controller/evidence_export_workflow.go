package controller

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/bundleanalysis"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type EvidenceAnalysisWorkflow struct {
	Status              string                                 `json:"status"`
	Unknown             []string                               `json:"unknown,omitempty"`
	ParentUsage         *bundleanalysis.ParentUsageObservation `json:"parent_usage,omitempty"`
	ParentUsageWindow   *EvidenceAnalysisWindow                `json:"parent_usage_window,omitempty"`
	ParentUsageCoverage string                                 `json:"parent_usage_coverage,omitempty"`
	Sources             []string                               `json:"sources,omitempty"`
	StatusStagesOmitted int                                    `json:"status_stages_omitted,omitempty"`
	Basis               string                                 `json:"basis"`
	TaskClock           EvidenceAnalysisTaskClock              `json:"task_clock"`
	StatusStages        []EvidenceAnalysisStatusStage          `json:"status_stages,omitempty"`
	ModelStages         []EvidenceAnalysisModelStage           `json:"model_stages,omitempty"`
	Validations         []EvidenceAnalysisValidationSummary    `json:"validations,omitempty"`
	Interactions        EvidenceAnalysisInteractions           `json:"interactions"`
	Usage               []EvidenceAnalysisUsageSummary         `json:"usage,omitempty"`
	Regression          EvidenceAnalysisWorkflowRegression     `json:"regression"`
}

type EvidenceAnalysisTaskClock struct {
	Status   string    `json:"status"`
	Start    time.Time `json:"start,omitempty"`
	End      time.Time `json:"end,omitempty"`
	EndBasis string    `json:"end_basis,omitempty"`
	WallMS   int64     `json:"wall_ms,omitempty"`
	Basis    string    `json:"basis"`
}

type EvidenceAnalysisStatusStage struct {
	From       string    `json:"from"`
	To         string    `json:"to"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	Unknown    string    `json:"unknown,omitempty"`
}

type EvidenceAnalysisModelStage struct {
	Role             string `json:"role"`
	Calls            int    `json:"calls"`
	WallMS           int64  `json:"wall_ms"`
	WorkMS           int64  `json:"work_ms"`
	UnknownDurations int    `json:"unknown_durations,omitempty"`
	Retried          int    `json:"retried,omitempty"`
	Interrupted      int    `json:"interrupted,omitempty"`
	SlowestMS        int64  `json:"slowest_ms,omitempty"`
}

type EvidenceAnalysisValidationSummary struct {
	UnknownDurations   int    `json:"unknown_durations,omitempty"`
	Form               string `json:"form"`
	Runs               int    `json:"runs"`
	Pass               int    `json:"pass,omitempty"`
	Fail               int    `json:"fail,omitempty"`
	Interrupted        int    `json:"interrupted,omitempty"`
	DurationMS         int64  `json:"duration_ms"`
	WallMS             int64  `json:"wall_ms"`
	TimingsCaptured    int    `json:"timings_captured,omitempty"`
	ToolchainsRecorded int    `json:"toolchains_recorded,omitempty"`
}

type EvidenceAnalysisInteractions struct {
	ReviewRounds  int            `json:"review_rounds"`
	AutoFixes     int            `json:"auto_fixes"`
	ResumedCalls  int            `json:"resumed_calls"`
	RetriedCalls  int            `json:"retried_calls,omitempty"`
	ProviderStops map[string]int `json:"provider_stops,omitempty"`
	Stops         map[string]int `json:"stops,omitempty"`
	Basis         string         `json:"basis"`
}

type EvidenceAnalysisUsageSummary struct {
	Side             string  `json:"side"`
	Source           string  `json:"source"`
	Coverage         string  `json:"coverage"`
	Role             string  `json:"role,omitempty"`
	Calls            int     `json:"calls"`
	UnknownCalls     int     `json:"unknown_calls,omitempty"`
	InputTokens      int64   `json:"input_tokens,omitempty"`
	CacheReadTokens  int64   `json:"cache_read_input_tokens,omitempty"`
	CacheWriteTokens int64   `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	Turns            int     `json:"turns,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

type EvidenceAnalysisWorkflowRegression struct {
	Status      string                               `json:"status"`
	Basis       string                               `json:"basis"`
	Conditions  EvidenceAnalysisWorkflowConditions   `json:"conditions"`
	Comparisons []EvidenceAnalysisWorkflowComparison `json:"comparisons,omitempty"`
	Unknown     string                               `json:"unknown,omitempty"`
}

type EvidenceAnalysisWorkflowConditions struct {
	RatioThreshold float64 `json:"ratio_threshold"`
	DeltaFloorMS   int64   `json:"delta_floor_ms"`
	CountFloor     int64   `json:"count_floor"`
	Candidates     int     `json:"candidates_bound"`
	ComparedPairs  string  `json:"compared_pairs"`
	Uncaptured     string  `json:"uncaptured"`
}

type EvidenceAnalysisWorkflowComparison struct {
	Metric    string  `json:"metric"`
	Candidate bool    `json:"candidate"`
	Latest    int64   `json:"latest"`
	Baseline  int64   `json:"baseline"`
	Delta     int64   `json:"delta"`
	Ratio     float64 `json:"ratio,omitempty"`
	Basis     string  `json:"basis"`
}

type workflowObservation struct {
	attemptID     string
	task          SemanticTaskRef
	telemetry     []state.ModelCallLog
	lifecycle     []state.TaskLifecycleRecord
	rounds        []state.RoundRecord
	runs          []collectedValidationRun
	window        *EvidenceAnalysisWindow
	attemptWindow *EvidenceAnalysisWindow
}

type workflowMetric struct {
	value    int64
	observed bool
}

type workflowInterval struct{ start, end time.Time }

const (
	evidenceAnalysisBasisWorkflow = "runtime-telemetry-lifecycle-rounds-validation-records"

	evidenceWorkflowUsageSide     = "worker-model"
	evidenceWorkflowUsageCoverage = "worker-side model calls only; parent Codex/Sol consumption is not collected in this store"

	evidenceWorkflowTaskWallMetric = "attempt_wall_ms"

	workflowNotComparableFloor = int64(-1)
)

const (
	workflowRegressionRatioThreshold  = 1.5
	workflowRegressionDeltaFloorMS    = int64(500)
	workflowRegressionCountFloor      = int64(2)
	workflowRegressionCandidatesBound = 20
)

func (o *workflowObservation) windowWallMS() int64 {
	if o.window == nil || o.window.Start.IsZero() || o.window.End.IsZero() {
		return 0
	}
	return o.window.End.Sub(o.window.Start).Milliseconds()
}

func (o *workflowObservation) resumedCalls() int64 {
	return int64(countWorkflowCalls(o.telemetry, func(call state.ModelCallLog) bool { return call.Resumed }))
}

func (o *workflowObservation) modelWallMS(role string) workflowMetric {
	for _, stage := range workflowModelStages(o.telemetry) {
		if stage.Role == role {
			return workflowMetric{value: stage.WallMS, observed: stage.UnknownDurations == 0}
		}
	}
	return workflowMetric{}
}

func (o *workflowObservation) usageInputTokens(role string) workflowMetric {
	for _, summary := range workflowUsageSummaries(o.telemetry) {
		if summary.Role == role {
			return workflowMetric{value: summary.InputTokens, observed: summary.UnknownCalls == 0}
		}
	}
	return workflowMetric{}
}

func (o *workflowObservation) validationDurationMS(form string) workflowMetric {
	for _, summary := range workflowValidationSummaries(o.runs) {
		if summary.Form == form {
			return workflowMetric{value: summary.DurationMS, observed: summary.UnknownDurations == 0}
		}
	}
	return workflowMetric{}
}

func (b *evidenceExportBuilder) analysisWorkflow(live EvidenceExportRuntimeSection) EvidenceAnalysisWorkflow {
	workflow := EvidenceAnalysisWorkflow{
		Status: evidenceAnalysisStatusAbsent,
		Basis:  evidenceAnalysisBasisWorkflow,
		TaskClock: EvidenceAnalysisTaskClock{
			Status: evidenceAnalysisStatusAbsent,
			Basis:  "runtime-task-lifecycle",
		},
		Interactions: EvidenceAnalysisInteractions{Basis: evidenceAnalysisBasisRounds},
		Regression:   emptyWorkflowRegression(evidenceAnalysisStatusAbsent),
	}
	latest := b.latestWorkflowObservation(b.workflowObservationWindow(live))
	if latest == nil {
		workflow.Regression.Unknown = "no valid complete runtime telemetry in this export"
		return workflow
	}
	workflow.Status = evidenceAnalysisStatusCollected
	if len(latest.telemetry) == 0 {
		workflow.Unknown = append(workflow.Unknown, "worker model telemetry unavailable or malformed; independent validation and parent observations retained")
	}
	if latest.rounds == nil {
		workflow.Unknown = append(workflow.Unknown, "review rounds unavailable or malformed")
	}
	if latest.lifecycle == nil {
		workflow.Unknown = append(workflow.Unknown, "lifecycle stages and stop counts unavailable or malformed")
	}
	workflow.TaskClock = workflowTaskClock(latest.window)
	if latest.window == nil {
		workflow.Unknown = append(workflow.Unknown, "complete Task lifecycle boundary unavailable; attempt window is not a Task clock")
	}
	workflow.StatusStages = workflowStatusStages(latest.lifecycle, latest.window)
	if len(workflow.StatusStages) > 64 {
		workflow.StatusStagesOmitted = len(workflow.StatusStages) - 64
		workflow.StatusStages = workflow.StatusStages[len(workflow.StatusStages)-64:]
	}
	workflow.ParentUsage = b.workflowParentUsage(latest.attemptWindow)
	workflow.ParentUsageWindow = latest.attemptWindow
	workflow.ParentUsageCoverage = "exported parent transcripts cover the selected attempt; counters do not represent the complete Task"
	workflow.Sources = b.workflowSources()
	workflow.ModelStages = workflowModelStages(latest.telemetry)
	workflow.Validations = workflowValidationSummaries(latest.runs)
	workflow.Interactions = workflowInteractions(latest)
	workflow.Usage = workflowUsageSummaries(latest.telemetry)
	workflow.Regression = b.workflowRegression(latest)
	return workflow
}

func emptyWorkflowRegression(status string) EvidenceAnalysisWorkflowRegression {
	return EvidenceAnalysisWorkflowRegression{
		Status: status,
		Basis:  evidenceAnalysisBasisWorkflow,
		Conditions: EvidenceAnalysisWorkflowConditions{
			RatioThreshold: workflowRegressionRatioThreshold,
			DeltaFloorMS:   workflowRegressionDeltaFloorMS,
			CountFloor:     workflowRegressionCountFloor,
			Candidates:     workflowRegressionCandidatesBound,
			ComparedPairs:  "latest attempt vs nearest predecessor attempt with sealed telemetry in the same export",
			Uncaptured:     "machine load, CPU model, and cache warmth are not captured; validation duration candidates additionally require equal recorded toolchains",
		},
	}
}

func (b *evidenceExportBuilder) latestWorkflowObservation(attemptWindow *EvidenceAnalysisWindow) *workflowObservation {
	taskID := b.workflowRuntimeTaskID()
	telemetry := parseWorkflowTelemetry(b.runtimeTelemetryBytes())
	lifecycle := parseWorkflowLifecycle(b.runtimeLifecycleBytes())
	rounds := parseWorkflowRounds(b.runtimeRoundsBytes())
	if !workflowTaskRecordsMatch(taskID, telemetry, lifecycle, rounds) {
		telemetry, lifecycle, rounds = nil, nil, nil
	}
	window := workflowLifecycleWindow(lifecycle, b.observedAt)
	runs := b.workflowTaskRuns(taskID, window)
	if len(telemetry) == 0 && window == nil && len(runs) == 0 && attemptWindow == nil {
		return nil
	}
	return &workflowObservation{
		task: b.targetTask, telemetry: workflowModelCalls(telemetry), lifecycle: lifecycle,
		rounds: rounds, runs: runs, window: window, attemptWindow: attemptWindow,
	}
}

func (b *evidenceExportBuilder) workflowRuntimeTaskID() string {
	if data, ok := b.runtimeFileBytes("state/task.id"); ok {
		return strings.TrimSpace(string(data))
	}
	if b.association != nil {
		return b.association.RuntimeTaskID
	}
	return b.attemptSection.RuntimeTaskID
}

func workflowTaskRecordsMatch(taskID string, calls []state.ModelCallLog, lifecycle []state.TaskLifecycleRecord, rounds []state.RoundRecord) bool {
	if taskID == "" {
		return false
	}
	for _, call := range calls {
		if call.TaskID != taskID {
			return false
		}
	}
	for _, record := range lifecycle {
		if record.TaskID != taskID {
			return false
		}
	}
	for _, record := range rounds {
		if record.TaskID != taskID {
			return false
		}
	}
	return true
}

func workflowLifecycleWindow(records []state.TaskLifecycleRecord, observedAt time.Time) *EvidenceAnalysisWindow {
	if len(records) == 0 || observedAt.IsZero() {
		return nil
	}
	ordered := append([]state.TaskLifecycleRecord(nil), records...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Timestamp.Before(ordered[j].Timestamp) })
	if ordered[0].From != "none" || ordered[0].Timestamp.IsZero() {
		return nil
	}
	for _, record := range ordered {
		if record.Timestamp.IsZero() || record.Timestamp.After(observedAt) {
			return nil
		}
	}
	window := &EvidenceAnalysisWindow{Start: ordered[0].Timestamp, End: observedAt, EndBasis: evidenceExportLiveEndBasis}
	last := ordered[len(ordered)-1]
	if last.To == string(state.TaskStatusComplete) {
		window.End, window.EndBasis = last.Timestamp, workflowTaskClockCompleteBasis
	}
	return window
}

func workflowModelCalls(calls []state.ModelCallLog) []state.ModelCallLog {
	var result []state.ModelCallLog
	seen := map[string]bool{}
	for _, call := range calls {
		if call.CallType != state.CallTypeTask && call.CallType != state.CallTypeProbe {
			continue
		}
		if call.CallID != "" && seen[call.CallID] {
			continue
		}
		seen[call.CallID] = true
		result = append(result, call)
	}
	return result
}

func (b *evidenceExportBuilder) workflowTaskRuns(taskID string, window *EvidenceAnalysisWindow) []collectedValidationRun {
	var runs []collectedValidationRun
	seen := map[string]bool{}
	for _, collected := range b.validationRuns {
		run := collected.run
		if taskID == "" || run.Form == "" || seen[run.ValidationRunID] {
			continue
		}
		if run.TaskID != "" {
			if run.TaskID != taskID {
				continue
			}
		} else if collected.binding != evidenceValidationBindingTaskStore && (window == nil || !workflowRunContained(run, window)) {
			continue
		}
		seen[run.ValidationRunID] = true
		runs = append(runs, collected)
	}
	return runs
}

func (b *evidenceExportBuilder) workflowObservationWindow(live EvidenceExportRuntimeSection) *EvidenceAnalysisWindow {
	switch {
	case b.attemptSection.Window != nil:
		return &EvidenceAnalysisWindow{Start: b.attemptSection.Window.Start, End: b.attemptSection.Window.End, EndBasis: b.attemptSection.Window.EndBasis}
	case live.Window != nil:
		return &EvidenceAnalysisWindow{Start: live.Window.Start, End: live.Window.End, EndBasis: live.Window.EndBasis}
	default:
		return nil
	}
}

func (b *evidenceExportBuilder) runtimeTelemetryBytes() ([]byte, bool) {
	if data, ok := b.runtimeFileBytes("task/telemetry.jsonl"); ok {
		return data, true
	}
	return b.fileBytes("attempt/telemetry.jsonl")
}

func (b *evidenceExportBuilder) runtimeLifecycleBytes() ([]byte, bool) {
	if data, ok := b.runtimeFileBytes("task/lifecycle.jsonl"); ok {
		return data, true
	}
	return b.fileBytes("attempt/lifecycle.jsonl")
}

func (b *evidenceExportBuilder) runtimeRoundsBytes() ([]byte, bool) {
	if data, ok := b.runtimeFileBytes("task/rounds.jsonl"); ok {
		return data, true
	}
	return b.fileBytes("attempt/rounds.jsonl")
}

func (b *evidenceExportBuilder) workflowWindowRuns(window *EvidenceAnalysisWindow) []collectedValidationRun {
	var runs []collectedValidationRun
	seen := map[string]bool{}
	for _, collected := range b.validationRuns {
		if collected.run.Form == "" || seen[collected.run.ValidationRunID] || !workflowRunContained(collected.run, window) {
			continue
		}
		seen[collected.run.ValidationRunID] = true
		runs = append(runs, collected)
	}
	return runs
}

func workflowRunContained(run qualitygate.RunRecord, window *EvidenceAnalysisWindow) bool {
	return window != nil && !window.Start.IsZero() && !window.End.IsZero() &&
		!run.StartedAt.IsZero() && !run.StartedAt.Before(window.Start) && run.StartedAt.Before(window.End) &&
		run.CompletedAt != nil && !run.CompletedAt.Before(run.StartedAt) && !run.CompletedAt.After(window.End)
}

func workflowTaskClock(window *EvidenceAnalysisWindow) EvidenceAnalysisTaskClock {
	clock := EvidenceAnalysisTaskClock{Status: evidenceAnalysisStatusAbsent, Basis: workflowTaskClockBasis}
	if window == nil || window.Start.IsZero() {
		return clock
	}
	clock.Status = evidenceAnalysisStatusCollected
	clock.Start = window.Start
	clock.End = window.End
	clock.EndBasis = window.EndBasis
	if !window.End.IsZero() {
		clock.WallMS = window.End.Sub(window.Start).Milliseconds()
	}
	return clock
}

func workflowStatusStages(lifecycle []state.TaskLifecycleRecord, window *EvidenceAnalysisWindow) []EvidenceAnalysisStatusStage {
	if len(lifecycle) == 0 {
		return nil
	}
	records := append([]state.TaskLifecycleRecord(nil), lifecycle...)
	sort.SliceStable(records, func(i, j int) bool { return records[i].Timestamp.Before(records[j].Timestamp) })
	end := time.Time{}
	if window != nil {
		end = window.End
	}
	stages := make([]EvidenceAnalysisStatusStage, 0, len(records))
	for index, record := range records {
		stageEnd := end
		if index+1 < len(records) {
			stageEnd = records[index+1].Timestamp
		}
		stage := EvidenceAnalysisStatusStage{From: record.From, To: record.To, StartedAt: record.Timestamp}
		if record.Timestamp.IsZero() || stageEnd.IsZero() || stageEnd.Before(record.Timestamp) {
			stage.Unknown = "stage start or end unavailable or invalid"
		} else {
			duration := stageEnd.Sub(record.Timestamp).Milliseconds()
			stage.DurationMS = &duration
		}
		stages = append(stages, stage)
	}
	return stages
}

func workflowModelStages(telemetry []state.ModelCallLog) []EvidenceAnalysisModelStage {
	byRole := map[string]*EvidenceAnalysisModelStage{}
	intervals := map[string][]workflowInterval{}
	for _, call := range workflowModelCalls(telemetry) {
		role := string(call.Role)
		stage := byRole[role]
		if stage == nil {
			stage = &EvidenceAnalysisModelStage{Role: role}
			byRole[role] = stage
		}
		stage.Calls++
		stage.WorkMS += call.WallDurationMS
		if !call.StartedAt.IsZero() && !call.CompletedAt.Before(call.StartedAt) && !call.CompletedAt.IsZero() {
			intervals[role] = append(intervals[role], workflowInterval{call.StartedAt, call.CompletedAt})
		} else {
			stage.UnknownDurations++
		}
		if call.WallDurationMS > stage.SlowestMS {
			stage.SlowestMS = call.WallDurationMS
		}
		if call.RetryOf != "" {
			stage.Retried++
		}
		if call.Outcome == "interrupted" {
			stage.Interrupted++
		}
	}
	roles := make([]string, 0, len(byRole))
	for role := range byRole {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	stages := make([]EvidenceAnalysisModelStage, 0, len(roles))
	for _, role := range roles {
		byRole[role].WallMS = workflowIntervalUnionMS(intervals[role])
		stages = append(stages, *byRole[role])
	}
	return stages
}

func workflowValidationSummaries(runs []collectedValidationRun) []EvidenceAnalysisValidationSummary {
	byForm := map[string]*EvidenceAnalysisValidationSummary{}
	intervals := map[string][]workflowInterval{}
	for _, collected := range runs {
		if collected.run.Form == "" {
			continue
		}
		summary := byForm[collected.run.Form]
		if summary == nil {
			summary = &EvidenceAnalysisValidationSummary{Form: collected.run.Form}
			byForm[collected.run.Form] = summary
		}
		summary.observe(collected)
		if workflowValidationDurationKnown(collected.run) {
			intervals[collected.run.Form] = append(intervals[collected.run.Form], workflowInterval{collected.run.StartedAt, *collected.run.CompletedAt})
		}
	}
	forms := make([]string, 0, len(byForm))
	for form := range byForm {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	summaries := make([]EvidenceAnalysisValidationSummary, 0, len(forms))
	for _, form := range forms {
		byForm[form].WallMS = workflowIntervalUnionMS(intervals[form])
		summaries = append(summaries, *byForm[form])
	}
	return summaries
}

func workflowInteractions(observation *workflowObservation) EvidenceAnalysisInteractions {
	interactions := EvidenceAnalysisInteractions{Basis: evidenceAnalysisBasisRounds}
	for _, record := range observation.rounds {
		interactions.ReviewRounds++
		interactions.AutoFixes += record.AutoFixes
	}
	interactions.ResumedCalls = int(countWorkflowCalls(observation.telemetry, func(call state.ModelCallLog) bool { return call.Resumed }))
	interactions.RetriedCalls = int(countWorkflowCalls(observation.telemetry, func(call state.ModelCallLog) bool { return call.RetryOf != "" }))
	interactions.ProviderStops = workflowCallClassCounts(observation.telemetry, func(call state.ModelCallLog) string { return call.ProviderClassification })
	interactions.Stops = workflowLifecycleStops(observation.lifecycle)
	return interactions
}

func countWorkflowCalls(telemetry []state.ModelCallLog, match func(state.ModelCallLog) bool) int {
	count := 0
	for _, call := range workflowModelCalls(telemetry) {
		if match(call) {
			count++
		}
	}
	return count
}

func workflowCallClassCounts(telemetry []state.ModelCallLog, class func(state.ModelCallLog) string) map[string]int {
	counts := map[string]int{}
	for _, call := range workflowModelCalls(telemetry) {
		if value := class(call); value != "" {
			counts[value]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func workflowLifecycleStops(lifecycle []state.TaskLifecycleRecord) map[string]int {
	counts := map[string]int{}
	for _, record := range lifecycle {
		switch record.To {
		case string(state.TaskStatusRateLimited), string(state.TaskStatusProviderUnavailable),
			string(state.TaskStatusInterrupted), string(state.TaskStatusUserTerminated):
			counts[record.To]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func workflowUsageSummaries(telemetry []state.ModelCallLog) []EvidenceAnalysisUsageSummary {
	byRole := map[string]*EvidenceAnalysisUsageSummary{}
	for _, call := range workflowModelCalls(telemetry) {
		role := string(call.Role)
		summary := byRole[role]
		if summary == nil {
			summary = &EvidenceAnalysisUsageSummary{
				Side: evidenceWorkflowUsageSide, Source: evidenceAnalysisBasisTelemetry,
				Coverage: evidenceWorkflowUsageCoverage, Role: role,
			}
			byRole[role] = summary
		}
		summary.Calls++
		if call.TopLevelUsage == (state.TokenUsage{}) {
			summary.UnknownCalls++
			summary.Coverage = "partial: provider counters missing for some calls; parent consumption is separate"
		}
		summary.InputTokens += call.TopLevelUsage.InputTokens
		summary.CacheReadTokens += call.TopLevelUsage.CacheReadInputTokens
		summary.CacheWriteTokens += call.TopLevelUsage.CacheCreationInputTokens
		summary.OutputTokens += call.TopLevelUsage.OutputTokens
		summary.Turns += call.TopLevelTurns
		summary.CostUSD += call.TotalCostUSD
	}
	roles := make([]string, 0, len(byRole))
	for role := range byRole {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	summaries := make([]EvidenceAnalysisUsageSummary, 0, len(roles))
	for _, role := range roles {
		summaries = append(summaries, *byRole[role])
	}
	return summaries
}

func (b *evidenceExportBuilder) workflowRegression(latest *workflowObservation) EvidenceAnalysisWorkflowRegression {
	regression := emptyWorkflowRegression(evidenceAnalysisStatusAbsent)
	latest = b.workflowAttemptObservation(latest)
	if latest == nil {
		regression.Unknown = "latest attempt scope or model conditions unavailable"
		return regression
	}
	predecessor := b.nearestComparablePredecessor(latest)
	if predecessor == nil {
		regression.Unknown = "no comparable predecessor attempt with sealed telemetry in this export"
		return regression
	}
	regression.Status = evidenceAnalysisStatusCollected
	pairBasis := "latest attempt vs predecessor " + predecessor.attemptID
	appendComparison := func(metric string, latestValue, baselineValue workflowMetric, floor int64) {
		if !latestValue.observed || !baselineValue.observed {
			return
		}
		regression.Comparisons = append(regression.Comparisons, workflowComparison(metric, latestValue.value, baselineValue.value, floor, pairBasis))
	}
	appendComparison(evidenceWorkflowTaskWallMetric,
		workflowMetric{value: latest.windowWallMS(), observed: latest.window != nil && latest.windowWallMS() > 0},
		workflowMetric{value: predecessor.windowWallMS(), observed: predecessor.window != nil && predecessor.windowWallMS() > 0},
		workflowRegressionDeltaFloorMS)
	appendComparison("review_rounds",
		workflowMetric{value: int64(len(latest.rounds)), observed: latest.rounds != nil},
		workflowMetric{value: int64(len(predecessor.rounds)), observed: predecessor.rounds != nil},
		workflowRegressionCountFloor)
	appendComparison("resumed_model_calls",
		workflowMetric{value: latest.resumedCalls(), observed: true},
		workflowMetric{value: predecessor.resumedCalls(), observed: true},
		workflowRegressionCountFloor)
	for _, stage := range workflowModelStages(latest.telemetry) {
		appendComparison("model:"+stage.Role+":wall_ms",
			workflowMetric{value: stage.WallMS, observed: stage.UnknownDurations == 0}, predecessor.modelWallMS(stage.Role),
			workflowRegressionDeltaFloorMS)
	}
	for _, summary := range workflowUsageSummaries(latest.telemetry) {
		appendComparison("usage:"+summary.Role+":input_tokens",
			workflowMetric{value: summary.InputTokens, observed: summary.UnknownCalls == 0}, predecessor.usageInputTokens(summary.Role),
			0)
	}
	for _, summary := range workflowValidationSummaries(latest.runs) {
		floor := workflowRegressionDeltaFloorMS
		if !workflowToolchainsComparable(latest.runs, predecessor.runs, summary.Form) {
			floor = workflowNotComparableFloor
		}
		appendComparison("validation:"+summary.Form+":duration_ms",
			workflowMetric{value: summary.DurationMS, observed: summary.UnknownDurations == 0}, predecessor.validationDurationMS(summary.Form),
			floor)
	}
	regression.Comparisons = boundWorkflowComparisons(regression.Comparisons)
	return regression
}

func workflowComparison(metric string, latestValue, baselineValue, floor int64, basis string) EvidenceAnalysisWorkflowComparison {
	comparison := EvidenceAnalysisWorkflowComparison{
		Metric: metric, Latest: latestValue, Baseline: baselineValue,
		Delta: latestValue - baselineValue, Basis: basis,
	}
	if floor == workflowNotComparableFloor {
		comparison.Basis = basis + " (recorded toolchains differ or are missing; not comparable)"
		return comparison
	}
	if latestValue > baselineValue && comparison.Delta >= floor && (baselineValue == 0 || float64(latestValue) >= float64(baselineValue)*workflowRegressionRatioThreshold) {
		comparison.Candidate = true
		if baselineValue > 0 {
			comparison.Ratio = roundWorkflowRatio(latestValue, baselineValue)
		}
	}
	return comparison
}

func roundWorkflowRatio(latest, baseline int64) float64 {
	if baseline <= 0 {
		return 0
	}
	return float64(int64(float64(latest)/float64(baseline)*100)) / 100
}

func boundWorkflowComparisons(comparisons []EvidenceAnalysisWorkflowComparison) []EvidenceAnalysisWorkflowComparison {
	sort.SliceStable(comparisons, func(i, j int) bool {
		if comparisons[i].Candidate != comparisons[j].Candidate {
			return comparisons[i].Candidate
		}
		if comparisons[i].Ratio != comparisons[j].Ratio {
			return comparisons[i].Ratio > comparisons[j].Ratio
		}
		return comparisons[i].Metric < comparisons[j].Metric
	})
	if len(comparisons) > workflowRegressionCandidatesBound {
		comparisons = comparisons[:workflowRegressionCandidatesBound]
	}
	return comparisons
}

func workflowToolchainsComparable(latest, baseline []collectedValidationRun, form string) bool {
	a, ok := workflowFormToolchain(latest, form)
	b, other := workflowFormToolchain(baseline, form)
	return ok && other && a == b
}

func workflowFormToolchain(runs []collectedValidationRun, form string) (qualitygate.RunToolchain, bool) {
	var recorded *qualitygate.RunToolchain
	for _, collected := range runs {
		if collected.run.Form != form {
			continue
		}
		t := collected.run.Toolchain
		if t == nil || !t.Recorded() {
			return qualitygate.RunToolchain{}, false
		}
		if recorded != nil && *recorded != *t {
			return qualitygate.RunToolchain{}, false
		}
		copy := *t
		recorded = &copy
	}
	if recorded == nil {
		return qualitygate.RunToolchain{}, false
	}
	return *recorded, true
}

func (b *evidenceExportBuilder) nearestComparablePredecessor(latest *workflowObservation) *workflowObservation {
	var sections []EvidenceExportPredecessorSection
	for _, section := range b.attemptSection.Predecessors {
		if section.Window != nil {
			sections = append(sections, section)
		}
	}
	sort.SliceStable(sections, func(i, j int) bool {
		if sections[i].Window.Start.Equal(sections[j].Window.Start) {
			return sections[i].AttemptID < sections[j].AttemptID
		}
		return sections[i].Window.Start.Before(sections[j].Window.Start)
	})
	for index := len(sections) - 1; index >= 0; index-- {
		section := sections[index]
		telemetry := parseWorkflowTelemetry(b.fileBytes(section.EntriesPrefix + "/telemetry.jsonl"))
		window := &EvidenceAnalysisWindow{Start: section.Window.Start, End: section.Window.End, EndBasis: section.Window.EndBasis}
		telemetry = workflowScopedCalls(telemetry, window)
		if len(telemetry) == 0 || !section.Task.Equal(latest.task) || !workflowModelsComparable(latest.telemetry, telemetry) {
			continue
		}
		return &workflowObservation{
			attemptID: section.AttemptID,
			task:      section.Task,
			telemetry: telemetry,
			lifecycle: parseWorkflowLifecycle(b.fileBytes(section.EntriesPrefix + "/lifecycle.jsonl")),
			rounds:    workflowScopedRounds(parseWorkflowRounds(b.fileBytes(section.EntriesPrefix+"/rounds.jsonl")), window),
			runs:      b.workflowWindowRuns(window),
			window:    window,
		}
	}
	return nil
}

func (b *evidenceExportBuilder) workflowAttemptObservation(task *workflowObservation) *workflowObservation {
	window := task.attemptWindow
	if window == nil || window.Start.IsZero() || window.End.IsZero() || !window.End.After(window.Start) {
		return nil
	}
	calls := workflowScopedCalls(task.telemetry, window)
	if len(calls) == 0 {
		return nil
	}
	return &workflowObservation{task: task.task, telemetry: calls, rounds: workflowScopedRounds(task.rounds, window), runs: b.workflowWindowRuns(window), window: window}
}

func workflowScopedCalls(calls []state.ModelCallLog, window *EvidenceAnalysisWindow) []state.ModelCallLog {
	var scoped []state.ModelCallLog
	for _, call := range workflowModelCalls(calls) {
		if call.StartedAt.IsZero() || call.CompletedAt.IsZero() || call.CompletedAt.Before(call.StartedAt) {
			return nil
		}
		if !call.StartedAt.Before(window.End) || !call.CompletedAt.After(window.Start) {
			continue
		}
		if call.StartedAt.Before(window.Start) || call.CompletedAt.After(window.End) {
			return nil
		}
		scoped = append(scoped, call)
	}
	return scoped
}

func workflowScopedRounds(rounds []state.RoundRecord, window *EvidenceAnalysisWindow) []state.RoundRecord {
	if rounds == nil {
		return nil
	}
	scoped := make([]state.RoundRecord, 0)
	for _, round := range rounds {
		if !round.CapturedAt.Before(window.Start) && round.CapturedAt.Before(window.End) {
			scoped = append(scoped, round)
		}
	}
	return scoped
}

func parseWorkflowTelemetry(data []byte, ok bool) []state.ModelCallLog {
	if !ok || len(data) == 0 {
		return nil
	}
	var logs []state.ModelCallLog
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var log state.ModelCallLog
		if err := json.Unmarshal([]byte(line), &log); err != nil {
			return nil
		}
		if log.CallID == "" || log.Version != state.ModelCallLogVersion {
			return nil
		}
		logs = append(logs, log)
	}
	return logs
}

func parseWorkflowLifecycle(data []byte, ok bool) []state.TaskLifecycleRecord {
	if !ok || len(data) == 0 {
		return nil
	}
	var records []state.TaskLifecycleRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseTaskLifecycleLine([]byte(line))
		if err != nil {
			return nil
		}
		records = append(records, record)
	}
	return records
}

func parseWorkflowRounds(data []byte, ok bool) []state.RoundRecord {
	if !ok {
		return nil
	}
	records := make([]state.RoundRecord, 0)
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var record state.RoundRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil
		}
		if record.Seq <= 0 || record.CapturedAt.IsZero() {
			return nil
		}
		records = append(records, record)
	}
	return records
}

func workflowModelsComparable(latest, baseline []state.ModelCallLog) bool {
	keys := func(calls []state.ModelCallLog) map[string]bool {
		result := map[string]bool{}
		for _, call := range calls {
			if call.ModelAlias == "" || call.Effort == "" {
				return nil
			}
			result[string(call.Role)+"|"+call.ModelAlias+"|"+call.ResolvedModelID+"|"+call.Effort] = true
		}
		return result
	}
	a, b := keys(latest), keys(baseline)
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

func (b *evidenceExportBuilder) workflowParentUsage(window *EvidenceAnalysisWindow) *bundleanalysis.ParentUsageObservation {
	var sources []bundleanalysis.ParentUsageEvidence
	for _, prefix := range []string{"live/transcripts/parent/", "bound/transcripts/parent/", "attempt/transcripts/parent/"} {
		for _, entry := range b.entries {
			if !strings.HasPrefix(entry.Path, prefix) {
				continue
			}
			if data, ok := b.fileBytes(entry.Path); ok {
				sources = append(sources, bundleanalysis.ParentUsageEvidence{Source: entry.Path, Content: data})
			}
		}
		if len(sources) > 0 {
			break
		}
	}
	var start, end time.Time
	if window != nil {
		start, end = window.Start, window.End
	}
	result := bundleanalysis.ObserveParentUsage(sources, start, end)
	return &result
}

func (b *evidenceExportBuilder) workflowSources() []string {
	var sources []string
	for _, entry := range b.entries {
		if strings.HasSuffix(entry.Path, "/telemetry.jsonl") || strings.HasSuffix(entry.Path, "/lifecycle.jsonl") || strings.HasSuffix(entry.Path, "/rounds.jsonl") {
			sources = append(sources, entry.Path)
		}
	}
	sort.Strings(sources)
	if len(sources) > 32 {
		sources = sources[:32]
	}
	return sources
}

func workflowIntervalUnionMS(intervals []workflowInterval) int64 {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	start, end := intervals[0].start, intervals[0].end
	var total int64
	for _, next := range intervals[1:] {
		if next.start.After(end) {
			total += end.Sub(start).Milliseconds()
			start, end = next.start, next.end
		} else if next.end.After(end) {
			end = next.end
		}
	}
	return total + end.Sub(start).Milliseconds()
}

func (summary *EvidenceAnalysisValidationSummary) observe(collected collectedValidationRun) {
	summary.Runs++
	if workflowValidationDurationKnown(collected.run) {
		summary.DurationMS += collected.run.DurationMS
	} else {
		summary.UnknownDurations++
	}
	switch collected.run.Status {
	case qualitygate.StatusPass:
		summary.Pass++
	case qualitygate.StatusFail:
		summary.Fail++
	case qualitygate.StatusInterrupted:
		summary.Interrupted++
	}
	if collected.timings != nil && collected.timings.Coverage.Captured {
		summary.TimingsCaptured++
	}
	if collected.run.Toolchain != nil && collected.run.Toolchain.Recorded() {
		summary.ToolchainsRecorded++
	}
}

func workflowValidationDurationKnown(run qualitygate.RunRecord) bool {
	if run.StartedAt.IsZero() || run.CompletedAt == nil || run.CompletedAt.Before(run.StartedAt) || run.Status == qualitygate.StatusRunning || run.DurationMS < 0 {
		return false
	}
	return run.DurationMS > 0 || run.CompletedAt.Sub(run.StartedAt) < time.Millisecond
}
