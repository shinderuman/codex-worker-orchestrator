package state

import (
	"fmt"
	"testing"
	"time"
)

func callOutliersBaseTime() time.Time {
	return time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
}

// callLogFixtureはtelemetry recordの最小fixture。session・phase・role・resumed・turn・
// durationを指定して分布計算の入力を作る。model aliasはroleから固定する。
func callLogFixture(sessionID string, phase string, role SessionRole, resumed bool, turns int, durationMS int64, startedAt time.Time) ModelCallLog {
	modelAlias := "opus"
	if role == ReviewerRole {
		modelAlias = "sonnet"
	}
	return ModelCallLog{
		Version: modelCallLogVersion, CallType: CallTypeTask, CallID: phase + "-" + startedAt.Format("150405.000000000"),
		TaskID: "task-fixture", SessionID: sessionID, StartedAt: startedAt,
		CompletedAt: startedAt.Add(time.Duration(durationMS) * time.Millisecond),
		Phase:       phase, Role: role, ModelAlias: modelAlias, Resumed: resumed,
		Outcome: "success", WallDurationMS: durationMS, TopLevelTurns: turns,
	}
}

func TestWorkerPhaseCategory(t *testing.T) {
	cases := map[string]string{
		"worker-new":                          WorkerPhaseCategoryNew,
		"worker-new-result-correct":           WorkerPhaseCategoryNew,
		"worker-explicit-fix":                 WorkerPhaseCategoryExplicitFix,
		"worker-explicit-fix-result-correct":  WorkerPhaseCategoryExplicitFix,
		"worker-auto-fix-1":                   WorkerPhaseCategoryAutoFix,
		"worker-auto-fix-2-result-correct":    WorkerPhaseCategoryAutoFix,
		"worker-report-only-1":                WorkerPhaseCategoryAutoFix,
		"worker-report-only-2-result-correct": WorkerPhaseCategoryAutoFix,
		"worker-decision":                     WorkerPhaseCategoryDecision,
		"worker-decision-result-correct":      WorkerPhaseCategoryDecision,
		"reviewer-1":                          "reviewer-1",
		"worker-unrelated":                    "worker-unrelated",
	}
	for phase, want := range cases {
		if got := WorkerPhaseCategory(phase); got != want {
			t.Errorf("WorkerPhaseCategory(%q) = %q want %q", phase, got, want)
		}
	}
}

func TestPercentileLinear(t *testing.T) {
	if got := percentileLinear(nil, 0.95); got != 0 {
		t.Fatalf("空valuesの分位 = %v want 0", got)
	}
	if got := percentileLinear([]int64{7}, 0.5); got != 7 {
		t.Fatalf("1点のmedian = %v want 7", got)
	}
	if got := percentileLinear([]int64{10, 100}, 0.95); got != 95.5 {
		t.Fatalf("2点補間 = %v want 95.5", got)
	}
	sorted := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 100}
	if got := percentileLinear(sorted, 0.5); got != 6 {
		t.Fatalf("11点のmedian = %v want 6", got)
	}
	if got := percentileLinear(sorted, 0.95); got != 55 {
		t.Fatalf("11点のp95 = %v want 55", got)
	}
}

// TestBuildCallOutlierReportSeparatesPhasesAndResumeはphase category・resumed別の分布、
// model/session集計、task増幅が保存recordだけから組めることを検証する。
func TestBuildCallOutlierReportSeparatesPhasesAndResume(t *testing.T) {
	base := callOutliersBaseTime()
	taskA := "aaaaaaaa-1111-4111-8111-111111111111"
	taskB := "bbbbbbbb-2222-4222-8222-222222222222"
	logs := []TaskCallLogs{
		{
			TaskID: taskA,
			Logs: []ModelCallLog{
				callLogFixture("sess-a", "worker-new", WorkerRole, false, 100, 1000, base),
				callLogFixture("sess-a", "worker-new", WorkerRole, true, 300, 7000, base.Add(time.Hour)),
				callLogFixture("sess-a", "worker-new-result-correct", WorkerRole, true, 2, 100, base.Add(2*time.Hour)),
				callLogFixture("sess-a", "worker-explicit-fix", WorkerRole, true, 50, 2000, base.Add(3*time.Hour)),
				callLogFixture("sess-ra", "reviewer-1", ReviewerRole, false, 10, 500, base.Add(4*time.Hour)),
				{Version: modelCallLogVersion, CallType: CallTypeEvent, TaskID: taskA, Phase: "parent-fix", Role: WorkerRole, StartedAt: base, CompletedAt: base},
				{Version: modelCallLogVersion, CallType: CallTypeProbe, TaskID: taskA, Phase: "worker-new-probe-1", Role: WorkerRole, StartedAt: base, CompletedAt: base},
				{Version: modelCallLogVersion, CallType: "unknown", TaskID: taskA, Phase: "legacy", Role: WorkerRole, StartedAt: base, CompletedAt: base},
			},
		},
		{
			TaskID: taskB,
			Logs: []ModelCallLog{
				callLogFixture("sess-b", "worker-new", WorkerRole, false, 30, 800, base.Add(5*time.Hour)),
				callLogFixture("sess-b", "worker-auto-fix-1", WorkerRole, true, 20, 600, base.Add(6*time.Hour)),
			},
		},
	}

	report := BuildCallOutlierReport(logs)

	if report.PercentileMethod != "linear" || report.MinPopulation != CallOutlierMinPopulation || report.OutlierRule == "" {
		t.Fatalf("reportの規則記述 = %#v", report)
	}
	if report.Records != (CallRecordCounts{Read: 10, Task: 7, Event: 1, Probe: 1, Other: 1}) {
		t.Fatalf("records = %#v", report.Records)
	}

	newCurrent := callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryNew, false)
	if newCurrent.Calls != 2 || newCurrent.Sessions != 2 || newCurrent.OutlierEligible {
		t.Fatalf("worker-new current group = %#v", newCurrent)
	}
	if newCurrent.Turns.Observed != 2 || newCurrent.Turns.Median != 65 || newCurrent.Turns.P95 != 96.5 ||
		newCurrent.Turns.Max != 100 || newCurrent.Turns.Total != 130 {
		t.Fatalf("worker-new current turns = %#v", newCurrent.Turns)
	}
	if newCurrent.DurationMS.Median != 900 || newCurrent.DurationMS.Total != 1800 || newCurrent.DurationMS.Max != 1000 {
		t.Fatalf("worker-new current durations = %#v", newCurrent.DurationMS)
	}

	newResume := callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryNew, true)
	if newResume.Calls != 2 || newResume.Sessions != 1 {
		t.Fatalf("worker-new resume group = %#v", newResume)
	}
	if len(newResume.RawPhases) != 2 || newResume.RawPhases["worker-new"] != 1 || newResume.RawPhases["worker-new-result-correct"] != 1 {
		t.Fatalf("worker-new resume raw_phases = %#v", newResume.RawPhases)
	}
	if newResume.Turns.Median != 151 || newResume.Turns.P95 != 285.1 || newResume.Turns.Total != 302 {
		t.Fatalf("worker-new resume turns = %#v", newResume.Turns)
	}
	callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryExplicitFix, true)
	callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryAutoFix, true)
	reviewer := callOutliersGroupOf(t, report.Distributions, "reviewer", "reviewer-1", false)
	if reviewer.Calls != 1 || reviewer.Turns.Median != 10 {
		t.Fatalf("reviewer group = %#v", reviewer)
	}

	workerModel := callOutliersModelOf(t, report.Models, "worker", "opus")
	if workerModel.Calls != 6 || workerModel.Turns.Total != 502 {
		t.Fatalf("worker model分布 = %#v", workerModel)
	}
	reviewerModel := callOutliersModelOf(t, report.Models, "reviewer", "sonnet")
	if reviewerModel.Calls != 1 || reviewerModel.DurationMS.Total != 500 {
		t.Fatalf("reviewer model分布 = %#v", reviewerModel)
	}
	if len(report.Models) != 2 {
		t.Fatalf("model分布 = %#v", report.Models)
	}

	session := callOutliersSessionOf(t, report.Sessions, "sess-a")
	if session.Role != WorkerRole || session.Calls != 4 || session.ResumedCalls != 3 ||
		session.TurnsTotal != 452 || session.DurationMSTotal != 10100 || session.Tasks != 1 {
		t.Fatalf("sess-a集計 = %#v", session)
	}
	if !session.FirstCallAt.Equal(base) || !session.LastCallAt.Equal(base.Add(3*time.Hour)) {
		t.Fatalf("sess-aの呼出窓 = %#v", session)
	}
	sessionB := callOutliersSessionOf(t, report.Sessions, "sess-b")
	if sessionB.Calls != 2 || sessionB.ResumedCalls != 1 || sessionB.TurnsTotal != 50 || sessionB.Tasks != 1 {
		t.Fatalf("sess-b集計 = %#v", sessionB)
	}

	if len(report.Tasks) != 2 {
		t.Fatalf("tasks = %#v", report.Tasks)
	}
	taskARow := report.Tasks[0]
	if taskARow.TaskID != taskA || taskARow.Calls != 4 || taskARow.ResumedCalls != 3 ||
		taskARow.TurnsTotal != 452 || taskARow.DurationMSTotal != 10100 {
		t.Fatalf("taskA増幅 = %#v", taskARow)
	}
	if taskARow.Initial.Phase != "worker-new" || !taskARow.Initial.StartedAt.Equal(base) ||
		taskARow.Initial.Turns != 100 || taskARow.Initial.DurationMS != 1000 || taskARow.Initial.Outcome != "success" {
		t.Fatalf("taskA initial = %#v", taskARow.Initial)
	}
	if taskARow.CallsAfterInitial != 3 || taskARow.TurnsXInitial == nil || *taskARow.TurnsXInitial != 4.52 ||
		taskARow.DurationMSXInitial == nil || *taskARow.DurationMSXInitial != 10.1 {
		t.Fatalf("taskA倍率 = %#v", taskARow)
	}
	categories := map[string]TaskCallCategoryTotal{}
	for _, category := range taskARow.ByCategory {
		categories[category.Category] = category
	}
	if len(categories) != 2 {
		t.Fatalf("taskA by_category = %#v", taskARow.ByCategory)
	}
	if categories[WorkerPhaseCategoryNew].Calls != 3 || categories[WorkerPhaseCategoryNew].Turns != 402 ||
		categories[WorkerPhaseCategoryNew].ResumedCalls != 2 {
		t.Fatalf("taskA worker-new category = %#v", categories[WorkerPhaseCategoryNew])
	}
	if categories[WorkerPhaseCategoryExplicitFix].Calls != 1 || categories[WorkerPhaseCategoryExplicitFix].Turns != 50 {
		t.Fatalf("taskA explicit-fix category = %#v", categories[WorkerPhaseCategoryExplicitFix])
	}
	if report.Tasks[1].TaskID != taskB || report.Tasks[1].TurnsTotal != 50 {
		t.Fatalf("taskB増幅 = %#v", report.Tasks[1])
	}

	if len(report.OutlierCalls) != 0 || len(report.OutlierTasks) != 0 {
		t.Fatalf("母数不足なのにoutlier = %#v / %#v", report.OutlierCalls, report.OutlierTasks)
	}
}

// TestBuildCallOutlierReportFlagsOutlierCallsAndTasksは母数が閾値を超えたgroup・task集団で
// p95超の呼出・taskを検出し、検出行に再現用の閾値とtask識別を載せることを検証する。
func TestBuildCallOutlierReportFlagsOutlierCallsAndTasks(t *testing.T) {
	base := callOutliersBaseTime()
	logs := make([]TaskCallLogs, 0, CallOutlierMinPopulation)
	for index := 0; index < CallOutlierMinPopulation; index++ {
		entries := []ModelCallLog{
			callLogFixture("sess-ringed", "worker-new", WorkerRole, false, 10, 1000, base.Add(time.Duration(index)*time.Minute)),
		}
		if index == 0 {
			entries = append(entries, callLogFixture("sess-ringed", "worker-new", WorkerRole, false, 100, 9000, base.Add(30*time.Minute)))
		}
		logs = append(logs, TaskCallLogs{TaskID: callOutliersTaskUUID(t, index), Logs: entries})
	}

	report := BuildCallOutlierReport(logs)

	group := callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryNew, false)
	if group.Calls != CallOutlierMinPopulation+1 || !group.OutlierEligible {
		t.Fatalf("worker-new current group = %#v", group)
	}
	// 昇順21値(10が20個・100が1個)のp95位置は19.0で、20番目要素の10そのものになる。
	if group.Turns.P95 != 10 {
		t.Fatalf("p95 = %v want 10", group.Turns.P95)
	}

	if len(report.OutlierCalls) != 1 {
		t.Fatalf("outlier_calls = %#v", report.OutlierCalls)
	}
	outlier := report.OutlierCalls[0]
	if outlier.Turns != 100 || outlier.Phase != "worker-new" || outlier.GroupPhase != WorkerPhaseCategoryNew ||
		outlier.GroupP95Turns != 10 || outlier.DurationMS != 9000 {
		t.Fatalf("outlier = %#v", outlier)
	}
	if outlier.TaskID != logs[0].TaskID || outlier.CallID == "" || outlier.ModelAlias != "opus" {
		t.Fatalf("outlierの再現識別 = %#v", outlier)
	}

	if len(report.Tasks) != CallOutlierMinPopulation {
		t.Fatalf("tasks = %d want %d", len(report.Tasks), CallOutlierMinPopulation)
	}
	// task turn合計は20task(10が19件・110が1件)で、p95位置18.05の補間値は15。
	if len(report.OutlierTasks) != 1 {
		t.Fatalf("outlier_tasks = %#v", report.OutlierTasks)
	}
	taskOutlier := report.OutlierTasks[0]
	if taskOutlier.TurnsTotal != 110 || taskOutlier.TasksP95Turns != 15 ||
		taskOutlier.TaskID != logs[0].TaskID || taskOutlier.Calls != 2 || taskOutlier.DurationMSTotal != 10000 {
		t.Fatalf("task outlier = %#v", taskOutlier)
	}
	for _, row := range report.Tasks {
		want := 1
		if row.TaskID == logs[0].TaskID {
			want = 2
		}
		if row.TurnsObservedCalls != want {
			t.Fatalf("task %sのturns_observed_calls = %d want %d", row.TaskID, row.TurnsObservedCalls, want)
		}
	}
}

// TestBuildCallOutlierReportTaskOutliersExcludeZeroOnlyTasksはtask-level outlierの母集団が
// 観測済みturn呼出を1件以上持つtaskだけであることを検証する。zero-only taskが母数を
// 見かけ上min_population以上へ増やしp95を0近くへ下げてもfalse outlierを出さない回帰固定。
func TestBuildCallOutlierReportTaskOutliersExcludeZeroOnlyTasks(t *testing.T) {
	base := callOutliersBaseTime()
	zeroOnly := CallOutlierMinPopulation + 5
	logs := make([]TaskCallLogs, 0, zeroOnly+2)
	for index := 0; index < zeroOnly; index++ {
		logs = append(logs, TaskCallLogs{TaskID: callOutliersTaskUUID(t, index), Logs: []ModelCallLog{
			callLogFixture(fmt.Sprintf("sess-z%d", index), "worker-new", WorkerRole, false, 0, 500, base.Add(time.Duration(index)*time.Minute)),
		}})
	}
	observedTurns := []int{10, 900}
	for index, turns := range observedTurns {
		logs = append(logs, TaskCallLogs{TaskID: callOutliersTaskUUID(t, 100+index), Logs: []ModelCallLog{
			callLogFixture(fmt.Sprintf("sess-o%d", index), "worker-new", WorkerRole, false, turns, 1000, base.Add(time.Duration(100+index)*time.Minute)),
		}})
	}

	report := BuildCallOutlierReport(logs)

	if len(report.Tasks) != zeroOnly+len(observedTurns) {
		t.Fatalf("tasks = %d want %d", len(report.Tasks), zeroOnly+len(observedTurns))
	}
	rows := make(map[string]TaskCallAmplification, len(report.Tasks))
	for _, row := range report.Tasks {
		rows[row.TaskID] = row
	}
	for index := 0; index < zeroOnly; index++ {
		if row := rows[callOutliersTaskUUID(t, index)]; row.TurnsObservedCalls != 0 || row.TurnsTotal != 0 {
			t.Fatalf("zero-only task %s = %#v", row.TaskID, row)
		}
	}
	for index := range observedTurns {
		if row := rows[callOutliersTaskUUID(t, 100+index)]; row.TurnsObservedCalls != 1 {
			t.Fatalf("観測済みtask %sのturns_observed_calls = %d want 1", row.TaskID, row.TurnsObservedCalls)
		}
	}
	// 全task数はmin_population超だが観測済みtaskは2件で母数不足のため、900 turn taskを
	// false outlierに出さない。
	if len(report.OutlierTasks) != 0 {
		t.Fatalf("観測済み母数不足なのにtask outlier = %#v", report.OutlierTasks)
	}
}

// TestBuildCallOutlierReportTaskOutliersKeepDetectionWithZeroOnlyは観測済みtaskが十分ある
// 場合の検出維持を検証する。zero-only taskを混ぜても母集団は観測済み21件のままなので
// p95=12となり高turn task1件だけを検出する。全taskを母集団にすればzero-onlyが母数を
// 水増ししてp95=10へ下がり、12 turn taskもfalse outlierに出る。
func TestBuildCallOutlierReportTaskOutliersKeepDetectionWithZeroOnly(t *testing.T) {
	base := callOutliersBaseTime()
	turnsPerTask := make([]int, 0, CallOutlierMinPopulation+11)
	for index := 0; index < CallOutlierMinPopulation-1; index++ {
		turnsPerTask = append(turnsPerTask, 10)
	}
	turnsPerTask = append(turnsPerTask, 12, 110)
	for index := 0; index < 30; index++ {
		turnsPerTask = append(turnsPerTask, 0)
	}
	logs := make([]TaskCallLogs, 0, len(turnsPerTask))
	for index, turns := range turnsPerTask {
		logs = append(logs, TaskCallLogs{TaskID: callOutliersTaskUUID(t, 200+index), Logs: []ModelCallLog{
			callLogFixture(fmt.Sprintf("sess-k%d", index), "worker-new", WorkerRole, false, turns, 1000, base.Add(time.Duration(index)*time.Minute)),
		}})
	}
	highTask := callOutliersTaskUUID(t, 200+CallOutlierMinPopulation)

	report := BuildCallOutlierReport(logs)

	if len(report.OutlierTasks) != 1 {
		t.Fatalf("outlier_tasks = %#v", report.OutlierTasks)
	}
	outlier := report.OutlierTasks[0]
	if outlier.TaskID != highTask || outlier.TurnsTotal != 110 || outlier.TasksP95Turns != 12 {
		t.Fatalf("task outlier = %#v want task %s 110 turn p95 12", outlier, highTask)
	}
}

// TestBuildCallOutlierReportEligibilityCountsObservedTurnsはgroup母数をturn観測済み呼出数で
// 数えることを検証する。中断などでturnを観測できなかった呼出が多く、観測値が母数下限に
// 届かないgroupは呼出数だけが多くても閾値根拠にしない。あわせて、分布母数が十分な
// reviewer groupからもoutlier呼出を出さない(規則がworker呼出限定)ことを固定する。
func TestBuildCallOutlierReportEligibilityCountsObservedTurns(t *testing.T) {
	base := callOutliersBaseTime()
	entries := make([]ModelCallLog, 0, 41)
	for index := 0; index < 18; index++ {
		entries = append(entries, callLogFixture("sess-obs", "worker-new", WorkerRole, false, 0, 500, base.Add(time.Duration(index)*time.Minute)))
	}
	entries = append(entries,
		callLogFixture("sess-obs", "worker-new", WorkerRole, false, 10, 1000, base.Add(20*time.Minute)),
		callLogFixture("sess-obs", "worker-new", WorkerRole, false, 100, 9000, base.Add(21*time.Minute)),
	)
	for index := 0; index < 20; index++ {
		entries = append(entries, callLogFixture("sess-obs-r", "reviewer-1", ReviewerRole, false, 10, 300, base.Add(time.Duration(40+index)*time.Minute)))
	}
	entries = append(entries, callLogFixture("sess-obs-r", "reviewer-1", ReviewerRole, false, 500, 9000, base.Add(61*time.Minute)))
	logs := []TaskCallLogs{{TaskID: callOutliersTaskUUID(t, 40), Logs: entries}}

	report := BuildCallOutlierReport(logs)

	workerGroup := callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryNew, false)
	if workerGroup.Calls != 20 || workerGroup.Turns.Observed != 2 || workerGroup.OutlierEligible {
		t.Fatalf("turn観測が薄いgroup = %#v", workerGroup)
	}
	reviewerGroup := callOutliersGroupOf(t, report.Distributions, "reviewer", "reviewer-1", false)
	if reviewerGroup.Calls != 21 || reviewerGroup.Turns.Observed != 21 || !reviewerGroup.OutlierEligible {
		t.Fatalf("reviewer group = %#v", reviewerGroup)
	}
	if len(report.OutlierCalls) != 0 || len(report.OutlierTasks) != 0 {
		t.Fatalf("観測母数不足group・reviewer呼出からoutlier = %#v / %#v", report.OutlierCalls, report.OutlierTasks)
	}
}

// TestBuildCallOutlierReportSmallRepositoryFixtureは少数呼出のrepository(8呼出程度の
// media-backupのような運用)へ同じ集計がそのまま適用でき、母数不足のためoutlier閾値を
// 出さないことを検証する。少数sampleを閾値根拠にしない運用契約の固定である。
func TestBuildCallOutlierReportSmallRepositoryFixture(t *testing.T) {
	base := callOutliersBaseTime()
	taskID := callOutliersTaskUUID(t, 90)
	logs := []TaskCallLogs{
		{TaskID: taskID, Logs: []ModelCallLog{
			callLogFixture("sess-small", "worker-new", WorkerRole, false, 40, 5000, base),
			callLogFixture("sess-small", "worker-new", WorkerRole, true, 60, 8000, base.Add(time.Hour)),
			callLogFixture("sess-small", "worker-explicit-fix", WorkerRole, true, 20, 3000, base.Add(2*time.Hour)),
			callLogFixture("sess-small", "worker-auto-fix-1", WorkerRole, true, 15, 2000, base.Add(3*time.Hour)),
			callLogFixture("sess-small-r", "reviewer-1", ReviewerRole, false, 12, 900, base.Add(4*time.Hour)),
			callLogFixture("sess-small-r", "reviewer-2", ReviewerRole, true, 8, 700, base.Add(5*time.Hour)),
			callLogFixture("sess-small", "worker-decision", WorkerRole, true, 30, 2500, base.Add(6*time.Hour)),
			callLogFixture("sess-small", "worker-explicit-fix-result-correct", WorkerRole, true, 2, 100, base.Add(7*time.Hour)),
		}},
	}

	report := BuildCallOutlierReport(logs)

	if report.Records.Task != 8 || len(report.Distributions) != 7 {
		t.Fatalf("小規模fixture集計 = %#v / %#v", report.Records, report.Distributions)
	}
	for _, group := range report.Distributions {
		if group.Calls >= CallOutlierMinPopulation || group.OutlierEligible {
			t.Fatalf("小規模fixtureのgroupが閾値母数を持っています: %#v", group)
		}
	}
	if len(report.OutlierCalls) != 0 || len(report.OutlierTasks) != 0 {
		t.Fatalf("小規模fixtureでoutlier = %#v / %#v", report.OutlierCalls, report.OutlierTasks)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].TurnsTotal != 167 || report.Tasks[0].Calls != 6 {
		t.Fatalf("小規模fixtureのtask増幅 = %#v", report.Tasks)
	}
	if report.MinPopulation != CallOutlierMinPopulation {
		t.Fatalf("min_population = %d", report.MinPopulation)
	}
}

// TestBuildCallOutlierReportZeroInitialTurnsは最初のworker呼出がturn・duration観測なしに
// 中断したtaskで両倍率をnullへ出し、観測できない計量を分布へ混ぜないことを検証する。
func TestBuildCallOutlierReportZeroInitialTurns(t *testing.T) {
	base := callOutliersBaseTime()
	logs := []TaskCallLogs{
		{TaskID: "cccccccc-3333-4333-8333-333333333333", Logs: []ModelCallLog{
			callLogFixture("sess-zero", "worker-new", WorkerRole, false, 0, 0, base),
			callLogFixture("sess-zero", "worker-new", WorkerRole, true, 120, 20000, base.Add(time.Hour)),
		}},
	}

	report := BuildCallOutlierReport(logs)

	group := callOutliersGroupOf(t, report.Distributions, "worker", WorkerPhaseCategoryNew, false)
	if group.Calls != 1 || group.Turns.Observed != 0 || group.Turns.Median != 0 || group.Turns.Total != 0 {
		t.Fatalf("turn未観測呼出の分布 = %#v", group)
	}
	if group.DurationMS.Observed != 0 || group.DurationMS.Total != 0 {
		t.Fatalf("duration未観測呼出の分布 = %#v", group)
	}
	row := report.Tasks[0]
	if row.TurnsTotal != 120 || row.Initial.Turns != 0 || row.Initial.DurationMS != 0 {
		t.Fatalf("task増幅 = %#v", row)
	}
	if row.TurnsXInitial != nil {
		t.Fatalf("turn数0のinitialで倍率 = %#v", *row.TurnsXInitial)
	}
	if row.DurationMSXInitial != nil {
		t.Fatalf("duration 0のinitialでduration倍率 = %#v", *row.DurationMSXInitial)
	}
}

func callOutliersGroupOf(t *testing.T, distributions []CallGroupDistribution, role string, phase string, resumed bool) CallGroupDistribution {
	t.Helper()
	for _, group := range distributions {
		if group.Role == role && group.Phase == phase && group.Resumed == resumed {
			return group
		}
	}
	t.Fatalf("分布にrole=%q phase=%q resumed=%vがありません: %#v", role, phase, resumed, distributions)
	return CallGroupDistribution{}
}

func callOutliersModelOf(t *testing.T, models []CallModelDistribution, role string, alias string) CallModelDistribution {
	t.Helper()
	for _, model := range models {
		if model.Role == role && model.ModelAlias == alias {
			return model
		}
	}
	t.Fatalf("model分布にrole=%q alias=%qがありません: %#v", role, alias, models)
	return CallModelDistribution{}
}

func callOutliersSessionOf(t *testing.T, sessions []CallSessionDistribution, sessionID string) CallSessionDistribution {
	t.Helper()
	for _, session := range sessions {
		if session.SessionID == sessionID {
			return session
		}
	}
	t.Fatalf("session集計に%qがありません: %#v", sessionID, sessions)
	return CallSessionDistribution{}
}

// callOutliersTaskUUIDはindexごとに一意なUUID v4生成形式のtask IDを返す。
func callOutliersTaskUUID(t *testing.T, index int) string {
	t.Helper()
	taskID := fmt.Sprintf("%08x-1111-4111-8111-%012x", index, index)
	if !ValidGeneratedUUID(taskID) {
		t.Fatalf("fixture task ID %qがUUID v4生成形式ではありません", taskID)
	}
	return taskID
}
