package state

import (
	"math"
	"slices"
	"strings"
	"time"
)

type TaskCallLogs struct {
	TaskID string
	Logs   []ModelCallLog
}

type CallRecordCounts struct {
	Read  int `json:"read"`
	Task  int `json:"task_calls"`
	Event int `json:"event_records"`
	Probe int `json:"probe_records"`
	Other int `json:"other_records"`
}

type CallMetricDistribution struct {
	Observed int     `json:"observed"`
	Median   float64 `json:"median"`
	P95      float64 `json:"p95"`
	Max      int64   `json:"max"`
	Total    int64   `json:"total"`
}

type CallGroupDistribution struct {
	Role            string                 `json:"role"`
	Phase           string                 `json:"phase"`
	Resumed         bool                   `json:"resumed"`
	Calls           int                    `json:"calls"`
	Turns           CallMetricDistribution `json:"turns"`
	DurationMS      CallMetricDistribution `json:"duration_ms"`
	Models          map[string]int         `json:"models"`
	RawPhases       map[string]int         `json:"raw_phases"`
	Sessions        int                    `json:"sessions"`
	OutlierEligible bool                   `json:"outlier_eligible"`
}

type CallModelDistribution struct {
	Role       string                 `json:"role"`
	ModelAlias string                 `json:"model_alias"`
	Calls      int                    `json:"calls"`
	Turns      CallMetricDistribution `json:"turns"`
	DurationMS CallMetricDistribution `json:"duration_ms"`
}

type CallSessionDistribution struct {
	SessionID       string      `json:"session_id"`
	Role            SessionRole `json:"role"`
	Models          []string    `json:"models"`
	Tasks           int         `json:"tasks"`
	Calls           int         `json:"calls"`
	ResumedCalls    int         `json:"resumed_calls"`
	TurnsTotal      int64       `json:"turns_total"`
	DurationMSTotal int64       `json:"duration_ms_total"`
	FirstCallAt     time.Time   `json:"first_call_at"`
	LastCallAt      time.Time   `json:"last_call_at"`
}

type TaskCallCategoryTotal struct {
	Category     string `json:"category"`
	Calls        int    `json:"calls"`
	ResumedCalls int    `json:"resumed_calls"`
	Turns        int64  `json:"turns"`
	DurationMS   int64  `json:"duration_ms"`
}

type TaskInitialCall struct {
	Phase      string    `json:"phase"`
	StartedAt  time.Time `json:"started_at"`
	Turns      int64     `json:"turns"`
	DurationMS int64     `json:"duration_ms"`
	Outcome    string    `json:"outcome"`
}

type TaskCallAmplification struct {
	TaskID             string                  `json:"task_id"`
	Calls              int                     `json:"calls"`
	ResumedCalls       int                     `json:"resumed_calls"`
	TurnsObservedCalls int                     `json:"turns_observed_calls"`
	TurnsTotal         int64                   `json:"turns_total"`
	DurationMSTotal    int64                   `json:"duration_ms_total"`
	Initial            TaskInitialCall         `json:"initial"`
	CallsAfterInitial  int                     `json:"calls_after_initial"`
	TurnsXInitial      *float64                `json:"turns_x_initial"`
	DurationMSXInitial *float64                `json:"duration_ms_x_initial"`
	ByCategory         []TaskCallCategoryTotal `json:"by_category"`
}

type CallOutlierObservation struct {
	TaskID        string    `json:"task_id"`
	CallID        string    `json:"call_id"`
	Phase         string    `json:"phase"`
	GroupPhase    string    `json:"group_phase"`
	Resumed       bool      `json:"resumed"`
	ModelAlias    string    `json:"model_alias"`
	SessionID     string    `json:"session_id"`
	StartedAt     time.Time `json:"started_at"`
	Turns         int64     `json:"turns"`
	DurationMS    int64     `json:"duration_ms"`
	GroupP95Turns float64   `json:"group_p95_turns"`
}

type TaskOutlierObservation struct {
	TaskID          string  `json:"task_id"`
	Calls           int     `json:"calls"`
	TurnsTotal      int64   `json:"turns_total"`
	DurationMSTotal int64   `json:"duration_ms_total"`
	TasksP95Turns   float64 `json:"tasks_p95_turns"`
}

type CallOutlierReport struct {
	PercentileMethod string                    `json:"percentile_method"`
	OutlierRule      string                    `json:"outlier_rule"`
	MinPopulation    int                       `json:"min_population"`
	Records          CallRecordCounts          `json:"records"`
	Distributions    []CallGroupDistribution   `json:"distributions"`
	Models           []CallModelDistribution   `json:"models"`
	Sessions         []CallSessionDistribution `json:"sessions"`
	Tasks            []TaskCallAmplification   `json:"tasks"`
	OutlierCalls     []CallOutlierObservation  `json:"outlier_calls"`
	OutlierTasks     []TaskOutlierObservation  `json:"outlier_tasks"`
}

type callGroupKey struct {
	role    string
	phase   string
	resumed bool
}

type callModelKey struct {
	role  string
	alias string
}

type callSessionKey struct {
	sessionID string
}

type callGroupSamples struct {
	calls     int
	turns     []int64
	durations []int64
	models    map[string]int
	rawPhases map[string]int
	sessions  map[string]bool
}

type workerCallRef struct {
	taskID string
	log    ModelCallLog
}

type callOutlierBuilder struct {
	report        CallOutlierReport
	groups        map[callGroupKey]*callGroupSamples
	modelSamples  map[callModelKey]*callGroupSamples
	sessionState  map[callSessionKey]*CallSessionDistribution
	sessionTasks  map[callSessionKey]map[string]bool
	sessionModels map[callSessionKey]map[string]bool
	taskWorkers   map[string][]ModelCallLog
	workerCalls   []workerCallRef
}

const callOutlierPercentileMethod = "linear"

const CallOutlierMinPopulation = 20

const callOutlierRule = "worker task call with top_level_turns > p95(turns) of its role+phase+resumed group; task with worker turns_total > p95(turns_total) across tasks having at least one observed-turn call; populations below min_population are ineligible"

const (
	WorkerPhaseCategoryNew         = "worker-new"
	WorkerPhaseCategoryExplicitFix = "worker-explicit-fix"
	WorkerPhaseCategoryAutoFix     = "worker-auto-fix"
	WorkerPhaseCategoryDecision    = "worker-decision"
)

const workerReportOnlyPhasePrefix = "worker-report-only-"

func WorkerPhaseCategory(phase string) string {
	switch {
	case phase == WorkerPhaseCategoryNew || strings.HasPrefix(phase, WorkerPhaseCategoryNew+"-"):
		return WorkerPhaseCategoryNew
	case strings.HasPrefix(phase, WorkerPhaseCategoryExplicitFix):
		return WorkerPhaseCategoryExplicitFix
	case strings.HasPrefix(phase, WorkerPhaseCategoryAutoFix+"-"),
		strings.HasPrefix(phase, workerReportOnlyPhasePrefix):
		return WorkerPhaseCategoryAutoFix
	case strings.HasPrefix(phase, WorkerPhaseCategoryDecision):
		return WorkerPhaseCategoryDecision
	default:
		return phase
	}
}

func BuildCallOutlierReport(tasks []TaskCallLogs) CallOutlierReport {
	builder := newCallOutlierBuilder()
	builder.absorbTasks(tasks)
	builder.buildGroupDistributions()
	builder.buildModelDistributions()
	builder.buildSessions()
	builder.buildTaskAmplifications()
	builder.sortReport()
	builder.report.OutlierTasks = taskTurnOutliers(builder.report.Tasks)
	return builder.report
}

func newCallOutlierBuilder() *callOutlierBuilder {
	return &callOutlierBuilder{
		report: CallOutlierReport{
			PercentileMethod: callOutlierPercentileMethod,
			OutlierRule:      callOutlierRule,
			MinPopulation:    CallOutlierMinPopulation,
			Distributions:    []CallGroupDistribution{},
			Models:           []CallModelDistribution{},
			Sessions:         []CallSessionDistribution{},
			Tasks:            []TaskCallAmplification{},
			OutlierCalls:     []CallOutlierObservation{},
			OutlierTasks:     []TaskOutlierObservation{},
		},
		groups:        make(map[callGroupKey]*callGroupSamples),
		modelSamples:  make(map[callModelKey]*callGroupSamples),
		sessionState:  make(map[callSessionKey]*CallSessionDistribution),
		sessionTasks:  make(map[callSessionKey]map[string]bool),
		sessionModels: make(map[callSessionKey]map[string]bool),
		taskWorkers:   make(map[string][]ModelCallLog),
		workerCalls:   make([]workerCallRef, 0),
	}
}

func (b *callOutlierBuilder) absorbTasks(tasks []TaskCallLogs) {
	for _, task := range tasks {
		for _, log := range task.Logs {
			b.absorbLog(task.TaskID, log)
		}
	}
}

func (b *callOutlierBuilder) absorbLog(taskID string, log ModelCallLog) {
	b.report.Records.Read++
	switch log.CallType {
	case CallTypeTask:
		b.report.Records.Task++
	case CallTypeEvent:
		b.report.Records.Event++
	case CallTypeProbe:
		b.report.Records.Probe++
	default:
		b.report.Records.Other++
	}
	if log.CallType != CallTypeTask {
		return
	}

	phase := log.Phase
	if log.Role == WorkerRole {
		phase = WorkerPhaseCategory(log.Phase)
		b.taskWorkers[taskID] = append(b.taskWorkers[taskID], log)
		b.workerCalls = append(b.workerCalls, workerCallRef{taskID: taskID, log: log})
	}
	groupKey := callGroupKey{role: string(log.Role), phase: phase, resumed: log.Resumed}
	b.groups[groupKey] = absorbCallGroup(b.groups[groupKey], log)
	modelKey := callModelKey{role: string(log.Role), alias: log.ModelAlias}
	b.modelSamples[modelKey] = absorbCallGroup(b.modelSamples[modelKey], log)
	b.absorbSession(taskID, log)
}

func (b *callOutlierBuilder) absorbSession(taskID string, log ModelCallLog) {
	key := callSessionKey{log.SessionID}
	session := b.sessionState[key]
	if session == nil {
		session = &CallSessionDistribution{
			SessionID:   log.SessionID,
			Role:        log.Role,
			FirstCallAt: log.StartedAt,
			LastCallAt:  log.StartedAt,
		}
		b.sessionState[key] = session
		b.sessionTasks[key] = make(map[string]bool)
		b.sessionModels[key] = make(map[string]bool)
	}
	session.Calls++
	if log.Resumed {
		session.ResumedCalls++
	}
	b.sessionTasks[key][taskID] = true
	b.sessionModels[key][log.ModelAlias] = true
	session.TurnsTotal += int64(log.TopLevelTurns)
	session.DurationMSTotal += log.WallDurationMS
	if log.StartedAt.Before(session.FirstCallAt) {
		session.FirstCallAt = log.StartedAt
	}
	if log.StartedAt.After(session.LastCallAt) {
		session.LastCallAt = log.StartedAt
	}
}

func (b *callOutlierBuilder) buildGroupDistributions() {
	eligible := make(map[callGroupKey]float64)
	for key, samples := range b.groups {
		turns := callMetricDistribution(samples.turns)
		b.report.Distributions = append(b.report.Distributions, CallGroupDistribution{
			Role:            key.role,
			Phase:           key.phase,
			Resumed:         key.resumed,
			Calls:           samples.calls,
			Turns:           turns,
			DurationMS:      callMetricDistribution(samples.durations),
			Models:          samples.models,
			RawPhases:       samples.rawPhases,
			Sessions:        len(samples.sessions),
			OutlierEligible: len(samples.turns) >= CallOutlierMinPopulation,
		})
		if key.role == string(WorkerRole) && len(samples.turns) >= CallOutlierMinPopulation {
			eligible[key] = turns.P95
		}
	}
	b.buildOutlierCallsFrom(eligible)
}

func (b *callOutlierBuilder) buildOutlierCallsFrom(groupTurnsP95 map[callGroupKey]float64) {
	for _, call := range b.workerCalls {
		key := callGroupKey{role: string(call.log.Role), phase: WorkerPhaseCategory(call.log.Phase), resumed: call.log.Resumed}
		p95, ok := groupTurnsP95[key]
		if !ok || float64(call.log.TopLevelTurns) <= p95 {
			continue
		}
		b.report.OutlierCalls = append(b.report.OutlierCalls, CallOutlierObservation{
			TaskID: call.taskID, CallID: call.log.CallID, Phase: call.log.Phase, GroupPhase: key.phase,
			Resumed: call.log.Resumed, ModelAlias: call.log.ModelAlias, SessionID: call.log.SessionID,
			StartedAt: call.log.StartedAt, Turns: int64(call.log.TopLevelTurns), DurationMS: call.log.WallDurationMS,
			GroupP95Turns: p95,
		})
	}
}

func (b *callOutlierBuilder) buildModelDistributions() {
	for key, samples := range b.modelSamples {
		b.report.Models = append(b.report.Models, CallModelDistribution{
			Role:       key.role,
			ModelAlias: key.alias,
			Calls:      samples.calls,
			Turns:      callMetricDistribution(samples.turns),
			DurationMS: callMetricDistribution(samples.durations),
		})
	}
}

func (b *callOutlierBuilder) buildSessions() {
	for key, session := range b.sessionState {
		session.Tasks = len(b.sessionTasks[key])
		session.Models = sortedKeys(b.sessionModels[key])
		b.report.Sessions = append(b.report.Sessions, *session)
	}
}

func (b *callOutlierBuilder) buildTaskAmplifications() {
	for taskID, logs := range b.taskWorkers {
		b.report.Tasks = append(b.report.Tasks, buildTaskAmplification(taskID, logs))
	}
}

func (b *callOutlierBuilder) sortReport() {
	slices.SortFunc(b.report.Distributions, compareCallGroupDistribution)
	slices.SortFunc(b.report.Models, compareCallModelDistribution)
	slices.SortFunc(b.report.Sessions, compareCallSessionDistribution)
	slices.SortFunc(b.report.Tasks, compareTaskCallAmplification)
	slices.SortFunc(b.report.OutlierCalls, compareCallOutlierObservation)
}

func compareCallGroupDistribution(a, b CallGroupDistribution) int {
	if a.Role != b.Role {
		return strings.Compare(a.Role, b.Role)
	}
	if a.Phase != b.Phase {
		return strings.Compare(a.Phase, b.Phase)
	}
	if a.Resumed == b.Resumed {
		return 0
	}
	if !a.Resumed {
		return -1
	}
	return 1
}

func compareCallModelDistribution(a, b CallModelDistribution) int {
	if a.Role != b.Role {
		return strings.Compare(a.Role, b.Role)
	}
	return strings.Compare(a.ModelAlias, b.ModelAlias)
}

func compareCallSessionDistribution(a, b CallSessionDistribution) int {
	if a.FirstCallAt.Equal(b.FirstCallAt) {
		return strings.Compare(a.SessionID, b.SessionID)
	}
	if a.FirstCallAt.Before(b.FirstCallAt) {
		return -1
	}
	return 1
}

func compareTaskCallAmplification(a, b TaskCallAmplification) int {
	if a.TurnsTotal == b.TurnsTotal {
		return strings.Compare(a.TaskID, b.TaskID)
	}
	if a.TurnsTotal > b.TurnsTotal {
		return -1
	}
	return 1
}

func compareCallOutlierObservation(a, b CallOutlierObservation) int {
	if a.Turns != b.Turns {
		if a.Turns > b.Turns {
			return -1
		}
		return 1
	}
	if a.TaskID != b.TaskID {
		return strings.Compare(a.TaskID, b.TaskID)
	}
	return strings.Compare(a.CallID, b.CallID)
}

func absorbCallGroup(samples *callGroupSamples, log ModelCallLog) *callGroupSamples {
	if samples == nil {
		samples = &callGroupSamples{
			models:    make(map[string]int),
			rawPhases: make(map[string]int),
			sessions:  make(map[string]bool),
		}
	}
	samples.calls++
	if log.TopLevelTurns > 0 {
		samples.turns = append(samples.turns, int64(log.TopLevelTurns))
	}
	if log.WallDurationMS > 0 {
		samples.durations = append(samples.durations, log.WallDurationMS)
	}
	if log.ModelAlias != "" {
		samples.models[log.ModelAlias]++
	}
	if log.Phase != "" {
		samples.rawPhases[log.Phase]++
	}
	if log.SessionID != "" {
		samples.sessions[log.SessionID] = true
	}
	return samples
}

func callMetricDistribution(values []int64) CallMetricDistribution {
	distribution := CallMetricDistribution{Observed: len(values)}
	if len(values) == 0 {
		return distribution
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	distribution.Median = roundMetricFloat(percentileLinear(sorted, 0.5))
	distribution.P95 = roundMetricFloat(percentileLinear(sorted, 0.95))
	distribution.Max = sorted[len(sorted)-1]
	for _, value := range sorted {
		distribution.Total += value
	}
	return distribution
}

func percentileLinear(sorted []int64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	position := q * float64(len(sorted)-1)
	lower := math.Floor(position)
	upper := math.Ceil(position)
	if lower == upper {
		return float64(sorted[int(lower)])
	}
	fraction := position - lower
	return float64(sorted[int(lower)]) + fraction*float64(sorted[int(upper)]-sorted[int(lower)])
}

func roundMetricFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

func buildTaskAmplification(taskID string, logs []ModelCallLog) TaskCallAmplification {
	sorted := slices.Clone(logs)
	slices.SortStableFunc(sorted, func(a, b ModelCallLog) int {
		if a.StartedAt.Equal(b.StartedAt) {
			return 0
		}
		if a.StartedAt.Before(b.StartedAt) {
			return -1
		}
		return 1
	})

	categoryOrder := make([]string, 0)
	byCategory := make(map[string]*TaskCallCategoryTotal)
	row := TaskCallAmplification{TaskID: taskID, ByCategory: []TaskCallCategoryTotal{}}
	for _, log := range sorted {
		accumulateTaskCall(&row, byCategory, &categoryOrder, log)
	}

	initial := sorted[0]
	row.Initial = TaskInitialCall{
		Phase: initial.Phase, StartedAt: initial.StartedAt,
		Turns: int64(initial.TopLevelTurns), DurationMS: initial.WallDurationMS,
		Outcome: initial.Outcome,
	}
	row.CallsAfterInitial = row.Calls - 1
	if initial.TopLevelTurns > 0 {
		timesX := roundMetricFloat(float64(row.TurnsTotal) / float64(initial.TopLevelTurns))
		row.TurnsXInitial = &timesX
	}
	if initial.WallDurationMS > 0 {
		timesX := roundMetricFloat(float64(row.DurationMSTotal) / float64(initial.WallDurationMS))
		row.DurationMSXInitial = &timesX
	}

	slices.Sort(categoryOrder)
	for _, category := range categoryOrder {
		row.ByCategory = append(row.ByCategory, *byCategory[category])
	}
	return row
}

func accumulateTaskCall(row *TaskCallAmplification, byCategory map[string]*TaskCallCategoryTotal, categoryOrder *[]string, log ModelCallLog) {
	category := WorkerPhaseCategory(log.Phase)
	total, ok := byCategory[category]
	if !ok {
		total = &TaskCallCategoryTotal{Category: category}
		byCategory[category] = total
		*categoryOrder = append(*categoryOrder, category)
	}
	total.Calls++
	if log.Resumed {
		total.ResumedCalls++
	}
	total.Turns += int64(log.TopLevelTurns)
	total.DurationMS += log.WallDurationMS

	row.Calls++
	if log.Resumed {
		row.ResumedCalls++
	}
	if log.TopLevelTurns > 0 {
		row.TurnsObservedCalls++
	}
	row.TurnsTotal += int64(log.TopLevelTurns)
	row.DurationMSTotal += log.WallDurationMS
}

func taskTurnOutliers(tasks []TaskCallAmplification) []TaskOutlierObservation {
	outliers := []TaskOutlierObservation{}
	observed := make([]TaskCallAmplification, 0, len(tasks))
	for _, task := range tasks {
		if task.TurnsObservedCalls > 0 {
			observed = append(observed, task)
		}
	}
	if len(observed) < CallOutlierMinPopulation {
		return outliers
	}
	totals := make([]int64, 0, len(observed))
	for _, task := range observed {
		totals = append(totals, task.TurnsTotal)
	}
	slices.Sort(totals)
	threshold := roundMetricFloat(percentileLinear(totals, 0.95))
	for _, task := range observed {
		if float64(task.TurnsTotal) > threshold {
			outliers = append(outliers, TaskOutlierObservation{
				TaskID: task.TaskID, Calls: task.Calls, TurnsTotal: task.TurnsTotal,
				DurationMSTotal: task.DurationMSTotal, TasksP95Turns: threshold,
			})
		}
	}
	slices.SortFunc(outliers, func(a, b TaskOutlierObservation) int {
		if a.TurnsTotal != b.TurnsTotal {
			if a.TurnsTotal > b.TurnsTotal {
				return -1
			}
			return 1
		}
		return strings.Compare(a.TaskID, b.TaskID)
	})
	return outliers
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
