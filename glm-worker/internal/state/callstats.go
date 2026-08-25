package state

import (
	"math"
	"slices"
	"strings"
	"time"
)

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

func BuildCallOutlierReport(tasks []TaskCallLogs) CallOutlierReport {
	report := CallOutlierReport{
		PercentileMethod: callOutlierPercentileMethod,
		OutlierRule:      callOutlierRule,
		MinPopulation:    CallOutlierMinPopulation,
		Distributions:    []CallGroupDistribution{},
		Models:           []CallModelDistribution{},
		Sessions:         []CallSessionDistribution{},
		Tasks:            []TaskCallAmplification{},
		OutlierCalls:     []CallOutlierObservation{},
		OutlierTasks:     []TaskOutlierObservation{},
	}

	groups := make(map[callGroupKey]*callGroupSamples)
	modelSamples := make(map[callModelKey]*callGroupSamples)
	sessionState := make(map[callSessionKey]*CallSessionDistribution)
	sessionTasks := make(map[callSessionKey]map[string]bool)
	sessionModels := make(map[callSessionKey]map[string]bool)
	taskWorkers := make(map[string][]ModelCallLog)
	workerCalls := make([]workerCallRef, 0)

	counts := CallRecordCounts{}
	for _, task := range tasks {
		for _, log := range task.Logs {
			counts.Read++
			switch log.CallType {
			case CallTypeTask:
				counts.Task++
			case CallTypeEvent:
				counts.Event++
			case CallTypeProbe:
				counts.Probe++
			default:
				counts.Other++
			}
			if log.CallType != CallTypeTask {
				continue
			}

			phase := log.Phase
			if log.Role == WorkerRole {
				phase = WorkerPhaseCategory(log.Phase)
				taskWorkers[task.TaskID] = append(taskWorkers[task.TaskID], log)
				workerCalls = append(workerCalls, workerCallRef{taskID: task.TaskID, log: log})
			}
			groupKey := callGroupKey{role: string(log.Role), phase: phase, resumed: log.Resumed}
			groups[groupKey] = absorbCallGroup(groups[groupKey], log)
			modelKey := callModelKey{role: string(log.Role), alias: log.ModelAlias}
			modelSamples[modelKey] = absorbCallGroup(modelSamples[modelKey], log)

			sessionKey := callSessionKey{log.SessionID}
			session := sessionState[sessionKey]
			if session == nil {
				session = &CallSessionDistribution{
					SessionID: log.SessionID, Role: log.Role,
					FirstCallAt: log.StartedAt, LastCallAt: log.StartedAt,
				}
				sessionState[sessionKey] = session
				sessionTasks[sessionKey] = make(map[string]bool)
				sessionModels[sessionKey] = make(map[string]bool)
			}
			session.Calls++
			if log.Resumed {
				session.ResumedCalls++
			}
			sessionTasks[sessionKey][task.TaskID] = true
			sessionModels[sessionKey][log.ModelAlias] = true
			session.TurnsTotal += int64(log.TopLevelTurns)
			session.DurationMSTotal += log.WallDurationMS
			if log.StartedAt.Before(session.FirstCallAt) {
				session.FirstCallAt = log.StartedAt
			}
			if log.StartedAt.After(session.LastCallAt) {
				session.LastCallAt = log.StartedAt
			}
		}
	}
	report.Records = counts

	groupTurnsP95 := make(map[callGroupKey]float64)
	for key, samples := range groups {
		turns := callMetricDistribution(samples.turns)
		durations := callMetricDistribution(samples.durations)
		report.Distributions = append(report.Distributions, CallGroupDistribution{
			Role:            key.role,
			Phase:           key.phase,
			Resumed:         key.resumed,
			Calls:           samples.calls,
			Turns:           turns,
			DurationMS:      durations,
			Models:          samples.models,
			RawPhases:       samples.rawPhases,
			Sessions:        len(samples.sessions),
			OutlierEligible: len(samples.turns) >= CallOutlierMinPopulation,
		})
		if key.role == string(WorkerRole) && len(samples.turns) >= CallOutlierMinPopulation {
			groupTurnsP95[key] = turns.P95
		}
	}

	for _, call := range workerCalls {
		key := callGroupKey{role: string(call.log.Role), phase: WorkerPhaseCategory(call.log.Phase), resumed: call.log.Resumed}
		p95, ok := groupTurnsP95[key]
		if !ok || float64(call.log.TopLevelTurns) <= p95 {
			continue
		}
		report.OutlierCalls = append(report.OutlierCalls, CallOutlierObservation{
			TaskID: call.taskID, CallID: call.log.CallID, Phase: call.log.Phase, GroupPhase: key.phase,
			Resumed: call.log.Resumed, ModelAlias: call.log.ModelAlias, SessionID: call.log.SessionID,
			StartedAt: call.log.StartedAt, Turns: int64(call.log.TopLevelTurns), DurationMS: call.log.WallDurationMS,
			GroupP95Turns: p95,
		})
	}

	for key, samples := range modelSamples {
		report.Models = append(report.Models, CallModelDistribution{
			Role:       key.role,
			ModelAlias: key.alias,
			Calls:      samples.calls,
			Turns:      callMetricDistribution(samples.turns),
			DurationMS: callMetricDistribution(samples.durations),
		})
	}

	for key, session := range sessionState {
		session.Tasks = len(sessionTasks[key])
		session.Models = sortedKeys(sessionModels[key])
		report.Sessions = append(report.Sessions, *session)
	}

	for taskID, logs := range taskWorkers {
		report.Tasks = append(report.Tasks, buildTaskAmplification(taskID, logs))
	}

	slices.SortFunc(report.Distributions, func(a, b CallGroupDistribution) int {
		if a.Role != b.Role {
			return strings.Compare(a.Role, b.Role)
		}
		if a.Phase != b.Phase {
			return strings.Compare(a.Phase, b.Phase)
		}
		if a.Resumed != b.Resumed {
			if !a.Resumed {
				return -1
			}
			return 1
		}
		return 0
	})
	slices.SortFunc(report.Models, func(a, b CallModelDistribution) int {
		if a.Role != b.Role {
			return strings.Compare(a.Role, b.Role)
		}
		return strings.Compare(a.ModelAlias, b.ModelAlias)
	})
	slices.SortFunc(report.Sessions, func(a, b CallSessionDistribution) int {
		if a.FirstCallAt != b.FirstCallAt {
			if a.FirstCallAt.Before(b.FirstCallAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.SessionID, b.SessionID)
	})
	slices.SortFunc(report.Tasks, func(a, b TaskCallAmplification) int {
		if a.TurnsTotal != b.TurnsTotal {
			if a.TurnsTotal > b.TurnsTotal {
				return -1
			}
			return 1
		}
		return strings.Compare(a.TaskID, b.TaskID)
	})
	slices.SortFunc(report.OutlierCalls, func(a, b CallOutlierObservation) int {
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
	})

	report.OutlierTasks = taskTurnOutliers(report.Tasks)
	return report
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
		category := WorkerPhaseCategory(log.Phase)
		total, ok := byCategory[category]
		if !ok {
			total = &TaskCallCategoryTotal{Category: category}
			byCategory[category] = total
			categoryOrder = append(categoryOrder, category)
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
