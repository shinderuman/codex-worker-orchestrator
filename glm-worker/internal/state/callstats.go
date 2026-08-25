package state

import (
	"math"
	"slices"
	"strings"
	"time"
)

// callOutlierPercentileMethodは分布の分位計算法。線形補間(rank法ではなくindex補間)で
// あり、昇順sort値の隣接2点間をq比率で内挿する。
const callOutlierPercentileMethod = "linear"

// CallOutlierMinPopulationはoutlier閾値算出に使う母数の下限。group(呼出単位)と
// task別turn合計(個別task単位)の両方へ適用する。group側の母数はturnを観測できた呼出数で
// 数え、閾値になる分位は観測値だけから計算するためである。これ未満の母集団は分布閾値の
// 根拠にならず(少数repoのsampleを閾値根拠にしない)、outlier判定から除外して分布値の表示
// だけを続ける。
const CallOutlierMinPopulation = 20

// callOutlierRuleはoutlier判定規則の機械可読説明。
const callOutlierRule = "worker task call with top_level_turns > p95(turns) of its role+phase+resumed group; task with worker turns_total > p95(turns_total) across tasks having at least one observed-turn call; populations below min_population are ineligible"

// worker phaseの集計category。raw phaseには同一段階の再出力(-result-correct)や
// auto-fixのround番号が混ざるため、分布はこのcategoryへ丸めてraw phaseは内訳で残す。
const (
	WorkerPhaseCategoryNew         = "worker-new"
	WorkerPhaseCategoryExplicitFix = "worker-explicit-fix"
	WorkerPhaseCategoryAutoFix     = "worker-auto-fix"
	WorkerPhaseCategoryDecision    = "worker-decision"
)

// workerReportOnlyPhasePrefixはTARGETS: PACKET再出力専用のauto-fix変種phase。収束上限・
// risk floor・routingは通常auto-fixと同じ枠で数えるため集計でも同じcategoryへ丸める。
const workerReportOnlyPhasePrefix = "worker-report-only-"

// WorkerPhaseCategoryはworker呼出のraw phaseを集計categoryへ対応付ける。
// reviewer phaseは丸めずraw phaseをそのまま使う。
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

// TaskCallLogsは1 task分のtelemetry呼出記録。分析側へ読み取り済みrecordを渡す。
type TaskCallLogs struct {
	TaskID string
	Logs   []ModelCallLog
}

// CallRecordCountsは分析対象recordのcall_type別計数。
type CallRecordCounts struct {
	Read  int `json:"read"`
	Task  int `json:"task_calls"`
	Event int `json:"event_records"`
	Probe int `json:"probe_records"`
	Other int `json:"other_records"`
}

// CallMetricDistributionは1計量の分布。Observedは計量が観測できたrecord数で、
// 観測できないrecord(turn数を持たないinterrupted呼出等)は分布へ入れずCalls側にだけ
// 数える。Median・P95は線形補間。Total・Maxは観測値の合計・最大。
type CallMetricDistribution struct {
	Observed int     `json:"observed"`
	Median   float64 `json:"median"`
	P95      float64 `json:"p95"`
	Max      int64   `json:"max"`
	Total    int64   `json:"total"`
}

// CallGroupDistributionは(role, phase, resumed)別の呼出分布。worker roleのPhaseは
// 集計categoryで、RawPhasesへraw phase別の呼出数を残す。OutlierEligibleは
// turn観測済み呼出がCallOutlierMinPopulation以上ありoutlier閾値算出に使えるか。
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

// CallModelDistributionは(role, model alias)別の呼出分布。
type CallModelDistribution struct {
	Role       string                 `json:"role"`
	ModelAlias string                 `json:"model_alias"`
	Calls      int                    `json:"calls"`
	Turns      CallMetricDistribution `json:"turns"`
	DurationMS CallMetricDistribution `json:"duration_ms"`
}

// CallSessionDistributionはsession別の呼出集計。同一sessionのcurrent/resume/fix呼出の
// 蓄積を横断で見るためのもので、追加のmodel呼出や推測補完は行わない。
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

// TaskCallCategoryTotalはtask内のcategory別呼出集計。
type TaskCallCategoryTotal struct {
	Category     string `json:"category"`
	Calls        int    `json:"calls"`
	ResumedCalls int    `json:"resumed_calls"`
	Turns        int64  `json:"turns"`
	DurationMS   int64  `json:"duration_ms"`
}

// TaskInitialCallはtask内で最初のworker Task Work Call。
type TaskInitialCall struct {
	Phase      string    `json:"phase"`
	StartedAt  time.Time `json:"started_at"`
	Turns      int64     `json:"turns"`
	DurationMS int64     `json:"duration_ms"`
	Outcome    string    `json:"outcome"`
}

// TaskCallAmplificationはtask単位のworker呼出増幅。TurnsXInitial・DurationMSXInitialは
// 合計を最初のworker呼出で割った倍率で、最初の呼出がturn数0で中断された場合は倍率を
// 観測できないためnullにする。TurnsObservedCallsはturnを観測できた呼出数で、task-level
// outlierの母集団はこの値が1以上のtaskだけとする。
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

// CallOutlierObservationはoutlierに当たったworker呼出1件。
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

// TaskOutlierObservationはoutlierに当たったtaskの累積worker呼出。
type TaskOutlierObservation struct {
	TaskID          string  `json:"task_id"`
	Calls           int     `json:"calls"`
	TurnsTotal      int64   `json:"turns_total"`
	DurationMSTotal int64   `json:"duration_ms_total"`
	TasksP95Turns   float64 `json:"tasks_p95_turns"`
}

// CallOutlierReportは保存済みtelemetryだけから組むworker呼出の分布とoutlier報告。
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

// callGroupKeyは呼出分布の集計key(role・phase・resumed)。
type callGroupKey struct {
	role    string
	phase   string
	resumed bool
}

// callModelKeyはmodel別分布の集計key(role・model alias)。
type callModelKey struct {
	role  string
	alias string
}

// callSessionKeyはsession別集計の集計key。
type callSessionKey struct {
	sessionID string
}

// callGroupSamplesは分布算出前の1集計単位への蓄積。
type callGroupSamples struct {
	calls     int
	turns     []int64
	durations []int64
	models    map[string]int
	rawPhases map[string]int
	sessions  map[string]bool
}

// workerCallRefはoutlier判定へ使いやすいようtask IDを付けたworker Task Work Call。
type workerCallRef struct {
	taskID string
	log    ModelCallLog
}

// BuildCallOutlierReportはtask別呼出記録から分布・増幅・outlierを組む。recordは読み取り
// 済みの現行versionだけを渡し、AI呼出・推測補完は行わない。並び順はrole・phaseの昇順、
// task・outlierはturn数の降順で固定する。
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

// absorbCallGroupは1呼出をgroupの集計へ反映する。nil samplesは新規に作る。
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

// percentileLinearは昇順sort済みvaluesのq分位を隣接2点の線形補間で返す。
// 観測がないときは0。
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

// roundMetricFloatは分位・倍率のようなfloat計量を小数2桁へ丸める。線形補間のfloat誤差を
// 出力へ残さないための表示丸めで、整数計量(Total・Max)は丸めない。
func roundMetricFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

// buildTaskAmplificationは1 taskのworker呼出から増幅行を組む。initialはStartedAtが
// 最も早い呼出で、同時刻のとき記録順の先を優先する。
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

// taskTurnOutliersはtask別worker turn合計のp95超taskを返す。母集団はgroup側と同じ
// 観測済み意味論でturnを観測できた呼出を1件以上持つtaskだけとし、zero-only taskが
// 母数を増やしp95を下げてfalse outlierを作らないようにする。母数が
// CallOutlierMinPopulation未満のときは閾値根拠にならないため空配列を返す。
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
