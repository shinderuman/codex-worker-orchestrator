package state

import (
	"fmt"
	"testing"
	"time"
)

func routingLogFixture(taskID string, sessionID string, phase string, role SessionRole, alias string, startedAt time.Time) ModelCallLog {
	return ModelCallLog{
		Version: modelCallLogVersion, CallType: CallTypeTask, TaskID: taskID, SessionID: sessionID,
		StartedAt: startedAt, CompletedAt: startedAt.Add(time.Minute),
		Phase: phase, Role: role, ModelAlias: alias, ResolvedModelID: "glm-5.3",
		Outcome: "success", PacketStatus: "IMPLEMENTED",
		ResolvedModelUsage: map[string]ResolvedModelUsage{
			"glm-5.3": {InputTokens: 1000, CacheReadInputTokens: 5000, OutputTokens: 200},
		},
	}
}

func routingCellOf(t *testing.T, cells []ModelRoutingCell, role string, phase string, risk string, delta string, alias string, model string) ModelRoutingCell {
	t.Helper()
	for _, cell := range cells {
		if cell.Role == role && cell.Phase == phase && cell.EffectiveRisk == risk &&
			cell.ConvergenceDelta == delta && cell.ModelAlias == alias && cell.ResolvedModel == model {
			return cell
		}
	}
	t.Fatalf("cell %s/%s/risk=%s/delta=%s/%s/%sがありません: %#v", role, phase, risk, delta, alias, model, cells)
	return ModelRoutingCell{}
}

func TestReviewerPhaseCategory(t *testing.T) {
	cases := map[string]string{
		"reviewer-1":                           ReviewerPhaseCategoryReview,
		"reviewer-12":                          ReviewerPhaseCategoryReview,
		"reviewer-2-result-correct":            ReviewerPhaseCategoryReview,
		"reviewer-1-risk-floor":                ReviewerPhaseCategoryRiskFloor,
		"reviewer-3-risk-floor-result-correct": ReviewerPhaseCategoryRiskFloor,
		"reviewer-1-high-floor":                ReviewerPhaseCategoryHighFloor,
		"reviewer-2-high-floor-result-correct": ReviewerPhaseCategoryHighFloor,
		"worker-new":                           "worker-new",
		"reviewer-report-only-1":               "reviewer-report-only-1",
		"reviewer-1-unexpected":                "reviewer-1-unexpected",
		"reviewer-":                            "reviewer-",
	}
	for phase, want := range cases {
		if got := ReviewerPhaseCategory(phase); got != want {
			t.Errorf("ReviewerPhaseCategory(%q) = %q want %q", phase, got, want)
		}
	}
}

func TestBuildModelRoutingReportAggregatesCells(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	taskA := "aaaaaaaa-1111-4111-8111-111111111111"
	taskB := "bbbbbbbb-2222-4222-8222-222222222222"

	workerA := routingLogFixture(taskA, "sess-w", "worker-new", WorkerRole, "opus", base)
	workerA.PacketStatus = "NEEDS_SOL_REVIEW"
	workerB := routingLogFixture(taskB, "sess-w2", "worker-new-result-correct", WorkerRole, "opus", base.Add(time.Hour))
	reviewer := routingLogFixture(taskA, "sess-r", "reviewer-2-risk-floor", ReviewerRole, "sonnet", base.Add(2*time.Hour))
	reviewer.Outcome = "accepted"
	reviewer.PacketStatus = "PASS"
	reviewer.EffectiveRisk = "HIGH"
	fallback := routingLogFixture(taskA, "sess-w", "worker-explicit-fix", WorkerRole, "opus", base.Add(3*time.Hour))
	fallback.ResolvedModelUsage = nil
	fallback.ResolvedModelID = "glm-4.7"
	fallback.TopLevelUsage = TokenUsage{InputTokens: 300, OutputTokens: 40}
	unusable := routingLogFixture(taskA, "sess-w", "worker-decision", WorkerRole, "opus", base.Add(4*time.Hour))
	unusable.ResolvedModelUsage = nil
	unusable.ResolvedModelID = ""
	unusable.TopLevelUsage = TokenUsage{}

	logs := []TaskCallLogs{
		{
			TaskID: taskA,
			Logs: []ModelCallLog{
				workerA, reviewer, fallback, unusable,
				{Version: modelCallLogVersion, CallType: CallTypeEvent, TaskID: taskA, Phase: "parent-fix", Role: WorkerRole, StartedAt: base, CompletedAt: base},
			},
		},
		{TaskID: taskB, Logs: []ModelCallLog{workerB}},
	}

	report := BuildModelRoutingReport(logs)

	if report.Records != (CallRecordCounts{Read: 6, Task: 5, Event: 1}) {
		t.Fatalf("records = %#v", report.Records)
	}
	if report.Sufficiency.MinQualityCalls != ModelRoutingMinQualityCallsPerGroup || report.Sufficiency.MinQualityTasks != ModelRoutingMinQualityTasksPerGroup {
		t.Fatalf("sufficiency = %#v", report.Sufficiency)
	}
	if report.Metrics.TreeUsage == "" || report.Metrics.Cost == "" || report.Metrics.Quality == "" || report.Sufficiency.Rule == "" {
		t.Fatalf("metric定義が空です: %#v", report.Metrics)
	}

	workerCell := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryNew, ModelRoutingUnknownRisk, RoundDeltaUnknown, "opus", "glm-5.3")
	if workerCell.Calls != 2 || workerCell.Tasks != 2 || workerCell.Sessions != 2 {
		t.Fatalf("worker cell = %#v", workerCell)
	}
	if workerCell.Usage.InputTokens != 2000 || workerCell.Usage.CacheReadInputTokens != 10000 || workerCell.Usage.OutputTokens != 400 {
		t.Fatalf("worker cell usage = %#v", workerCell.Usage)
	}
	if workerCell.UsageUnknownCalls != 0 || workerCell.TopLevelCalls != 2 {
		t.Fatalf("worker cell境界 = %#v", workerCell)
	}
	if workerCell.Outcomes["success"] != 2 || workerCell.PacketStatuses["NEEDS_SOL_REVIEW"] != 1 || workerCell.PacketStatuses["IMPLEMENTED"] != 1 {
		t.Fatalf("worker cell outcomes = %#v / %#v", workerCell.Outcomes, workerCell.PacketStatuses)
	}
	if workerCell.EffectiveRisk != ModelRoutingUnknownRisk || workerCell.ConvergenceDelta != RoundDeltaUnknown {
		t.Fatalf("worker cell軸 = %#v", workerCell)
	}

	reviewerCell := routingCellOf(t, report.Cells, "reviewer", ReviewerPhaseCategoryRiskFloor, "HIGH", RoundDeltaUnknown, "sonnet", "glm-5.3")
	if reviewerCell.Calls != 1 || reviewerCell.Outcomes["accepted"] != 1 || reviewerCell.PacketStatuses["PASS"] != 1 {
		t.Fatalf("reviewer cell = %#v", reviewerCell)
	}
	if reviewerCell.EffectiveRisk != "HIGH" {
		t.Fatalf("reviewer cell risk = %#v", reviewerCell)
	}

	fallbackCell := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryExplicitFix, ModelRoutingUnknownRisk, RoundDeltaUnknown, "opus", "glm-4.7")
	if fallbackCell.Usage.InputTokens != 300 || fallbackCell.Usage.OutputTokens != 40 || fallbackCell.UsageUnknownCalls != 0 {
		t.Fatalf("fallback cell usage = %#v", fallbackCell.Usage)
	}

	unknownCell := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryDecision, ModelRoutingUnknownRisk, RoundDeltaUnknown, "opus", ModelRoutingUnknownModel)
	if unknownCell.Usage != (TokenUsage{}) || unknownCell.UsageUnknownCalls != 1 {
		t.Fatalf("unknown cell = %#v", unknownCell)
	}

	if len(report.AliasLinks) != 2 {
		t.Fatalf("alias_links = %#v", report.AliasLinks)
	}
	if report.AliasLinks[0].Role != "reviewer" || report.AliasLinks[0].ModelAlias != "sonnet" ||
		report.AliasLinks[0].ResolvedModels["glm-5.3"] != 1 {
		t.Fatalf("reviewer alias link = %#v", report.AliasLinks[0])
	}
	if report.AliasLinks[1].Role != "worker" || report.AliasLinks[1].ModelAlias != "opus" ||
		report.AliasLinks[1].ResolvedModels["glm-5.3"] != 2 || report.AliasLinks[1].ResolvedModels["glm-4.7"] != 1 ||
		report.AliasLinks[1].ResolvedModels[ModelRoutingUnknownModel] != 1 {
		t.Fatalf("worker alias link = %#v", report.AliasLinks[1])
	}

	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaUnknown {
		t.Fatalf("quality delta = %#v", report.Evaluation)
	}
	if len(report.Evaluation.ComparableGroups) != 0 || len(report.QualityGroups) != 0 {
		t.Fatalf("quality groups = %#v / %#v", report.Evaluation.ComparableGroups, report.QualityGroups)
	}
	if len(report.Evaluation.Reasons) != 1 ||
		report.Evaluation.Reasons[0] != "no attributable downstream quality evidence; operational outcome and packet_status are not model-quality evidence" {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func TestBuildModelRoutingReportSingleModelIsUnknown(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	worker := routingLogFixture("aaaaaaaa-1111-4111-8111-111111111111", "sess-w", "worker-new", WorkerRole, "opus", base)
	worker.ResolvedModelUsage = nil
	worker.ResolvedModelID = ""
	worker.TopLevelUsage = TokenUsage{}
	logs := []TaskCallLogs{{TaskID: "aaaaaaaa-1111-4111-8111-111111111111", Logs: []ModelCallLog{
		routingLogFixture("aaaaaaaa-1111-4111-8111-111111111111", "sess-w2", "worker-new", WorkerRole, "opus", base.Add(30*time.Minute)),
		worker,
		routingLogFixture("aaaaaaaa-1111-4111-8111-111111111111", "sess-r", "reviewer-1", ReviewerRole, "sonnet", base.Add(time.Hour)),
	}}}

	report := BuildModelRoutingReport(logs)

	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaUnknown {
		t.Fatalf("quality delta = %#v", report.Evaluation)
	}
	if len(report.Evaluation.Reasons) != 1 ||
		report.Evaluation.Reasons[0] != "no attributable downstream quality evidence; operational outcome and packet_status are not model-quality evidence" {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func TestBuildModelRoutingReportNoTaskCalls(t *testing.T) {
	report := BuildModelRoutingReport(nil)

	if len(report.Cells) != 0 || len(report.AliasLinks) != 0 {
		t.Fatalf("空入力で集計がありました: %#v", report)
	}
	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaUnknown ||
		report.Evaluation.Reasons[0] != "no task-call telemetry recorded" {
		t.Fatalf("evaluation = %#v", report.Evaluation)
	}
}

func routingSufficientFixture(prefix string, alias string, model string, risk string, delta string, base time.Time) []TaskCallLogs {
	logs := make([]TaskCallLogs, 0, ModelRoutingMinQualityTasksPerGroup)
	callsPerTask := ModelRoutingMinQualityCallsPerGroup / ModelRoutingMinQualityTasksPerGroup
	for task := 0; task < ModelRoutingMinQualityTasksPerGroup; task++ {
		taskID := fmt.Sprintf("%s-1111-4111-8111-00000000000%d", prefix, task)
		entries := make([]ModelCallLog, 0, callsPerTask)
		deltas := make(map[string]string)
		quality := make(map[string]string)
		for call := 0; call < callsPerTask; call++ {
			callID := fmt.Sprintf("call-%s-%d-%d", prefix, task, call)
			entry := routingLogFixture(taskID, "sess-"+prefix, "worker-new", WorkerRole, alias, base.Add(time.Duration(task)*time.Hour+time.Duration(call)*time.Minute))
			entry.CallID = callID
			entry.EffectiveRisk = risk
			entry.ResolvedModelID = model
			entry.ResolvedModelUsage = map[string]ResolvedModelUsage{
				model: {InputTokens: 100, OutputTokens: 10},
			}
			entries = append(entries, entry)
			quality[callID] = ModelRoutingQualityReviewPass
			if delta != RoundDeltaUnknown {
				deltas[callID] = delta
			}
		}
		logs = append(logs, TaskCallLogs{TaskID: taskID, Logs: entries, ConvergenceDeltas: deltas, QualityOutcomes: quality})
	}
	return logs
}

func TestBuildModelRoutingReportComparableGroup(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	logs := append(
		routingSufficientFixture("aaaaaaaa", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base),
		routingSufficientFixture("bbbbbbbb", "haiku", "glm-4.7", "HIGH", RoundDeltaSemantic, base)...)

	report := BuildModelRoutingReport(logs)

	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaComparable {
		t.Fatalf("quality delta = %#v", report.Evaluation)
	}
	if len(report.Evaluation.ComparableGroups) != 1 ||
		report.Evaluation.ComparableGroups[0] != "worker/worker-new/risk=HIGH/delta=semantic-change: glm-4.7, glm-5.3" {
		t.Fatalf("comparable groups = %#v", report.Evaluation.ComparableGroups)
	}
	if len(report.Evaluation.Reasons) != 0 {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
	if len(report.QualityGroups) != 2 {
		t.Fatalf("quality groups = %#v", report.QualityGroups)
	}
	for _, group := range report.QualityGroups {
		if !group.Sufficient || group.EffectiveRisk != "HIGH" || group.ConvergenceDelta != RoundDeltaSemantic {
			t.Fatalf("quality group = %#v", group)
		}
	}
}

func TestBuildModelRoutingReportSeparatesRiskAndDeltaGroups(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	cases := map[string][]TaskCallLogs{
		"different effective risk": append(
			routingSufficientFixture("aaaaaaaa", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base),
			routingSufficientFixture("bbbbbbbb", "haiku", "glm-4.7", "LOW", RoundDeltaSemantic, base)...),
		"different convergence delta": append(
			routingSufficientFixture("aaaaaaaa", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base),
			routingSufficientFixture("bbbbbbbb", "haiku", "glm-4.7", "HIGH", RoundDeltaDocChange, base)...),
	}
	for name, logs := range cases {
		t.Run(name, func(t *testing.T) {
			report := BuildModelRoutingReport(logs)
			if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaInsufficient {
				t.Fatalf("quality delta = %#v", report.Evaluation)
			}
			if len(report.Evaluation.ComparableGroups) != 0 {
				t.Fatalf("comparable groups = %#v", report.Evaluation.ComparableGroups)
			}
			if len(report.Evaluation.Reasons) != 1 ||
				report.Evaluation.Reasons[0] != "no role+normalized-phase+effective-risk+convergence-delta group contains at least 2 of the observed resolved models" {
				t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
			}
			for _, group := range report.QualityGroups {
				if !group.Sufficient {
					t.Fatalf("quality group should meet its own sample floor: %#v", group)
				}
			}
		})
	}
}

func TestBuildModelRoutingReportConvergenceDeltaJoin(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	taskID := "aaaaaaaa-1111-4111-8111-111111111111"
	joined := routingLogFixture(taskID, "sess-w", "worker-new", WorkerRole, "opus", base)
	joined.CallID = "call-joined"
	missing := routingLogFixture(taskID, "sess-w", "worker-new", WorkerRole, "opus", base.Add(time.Minute))
	missing.CallID = "call-missing"
	noCallID := routingLogFixture(taskID, "sess-w", "worker-new", WorkerRole, "opus", base.Add(2*time.Minute))
	logs := []TaskCallLogs{{
		TaskID: taskID,
		Logs:   []ModelCallLog{joined, missing, noCallID},
		ConvergenceDeltas: map[string]string{
			"call-joined":  RoundDeltaSameSnapshot,
			"call-missing": "",
		},
	}}

	report := BuildModelRoutingReport(logs)

	joinedCell := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryNew, ModelRoutingUnknownRisk, RoundDeltaSameSnapshot, "opus", "glm-5.3")
	if joinedCell.Calls != 1 {
		t.Fatalf("joined cell = %#v", joinedCell)
	}
	unknownCell := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryNew, ModelRoutingUnknownRisk, RoundDeltaUnknown, "opus", "glm-5.3")
	if unknownCell.Calls != 2 {
		t.Fatalf("unknown delta cell = %#v", unknownCell)
	}
}

func TestBuildModelRoutingReportInsufficientContrast(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	small := []TaskCallLogs{{
		TaskID: "cccccccc-1111-4111-8111-111111111111",
		Logs: []ModelCallLog{
			routingLogFixture("cccccccc-1111-4111-8111-111111111111", "sess-c", "worker-new", WorkerRole, "haiku", base),
			routingLogFixture("cccccccc-1111-4111-8111-111111111111", "sess-c", "worker-new", WorkerRole, "haiku", base.Add(time.Minute)),
		},
	}}
	deltas := make(map[string]string)
	for index := range small[0].Logs {
		small[0].Logs[index].CallID = fmt.Sprintf("call-small-%d", index)
		small[0].Logs[index].EffectiveRisk = "HIGH"
		small[0].Logs[index].ResolvedModelID = "glm-4.7"
		small[0].Logs[index].ResolvedModelUsage = map[string]ResolvedModelUsage{
			"glm-4.7": {InputTokens: 100, OutputTokens: 10},
		}
		deltas[small[0].Logs[index].CallID] = RoundDeltaSemantic
	}
	small[0].ConvergenceDeltas = deltas
	small[0].QualityOutcomes = map[string]string{
		"call-small-0": ModelRoutingQualityReviewPass,
		"call-small-1": ModelRoutingQualityReviewPass,
	}
	logs := append(routingSufficientFixture("aaaaaaaa", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base), small...)

	report := BuildModelRoutingReport(logs)

	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaInsufficient {
		t.Fatalf("quality delta = %#v", report.Evaluation)
	}
	if len(report.Evaluation.ComparableGroups) != 0 {
		t.Fatalf("comparable groups = %#v", report.Evaluation.ComparableGroups)
	}
	want := "worker/worker-new/risk=HIGH/delta=semantic-change: glm-4.7 has 2 attributable quality calls across 1 tasks, below min 20 calls / 5 tasks"
	found := false
	for _, reason := range report.Evaluation.Reasons {
		if reason == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("不足reasonがありません: %#v", report.Evaluation.Reasons)
	}
}

func TestBuildModelRoutingReportMixedModelDoesNotDuplicateTopLevelVerdict(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	taskID := "dddddddd-1111-4111-8111-111111111111"
	log := routingLogFixture(taskID, "sess-mixed", "worker-new", WorkerRole, "opus", base)
	log.CallID = "call-mixed"
	log.EffectiveRisk = "HIGH"
	log.ResolvedModelID = "glm-5.3"
	log.ResolvedModelUsage = map[string]ResolvedModelUsage{
		"glm-5.3":      {InputTokens: 100, OutputTokens: 10},
		"helper-model": {InputTokens: 50, OutputTokens: 5},
	}
	report := BuildModelRoutingReport([]TaskCallLogs{{
		TaskID:            taskID,
		Logs:              []ModelCallLog{log},
		ConvergenceDeltas: map[string]string{"call-mixed": RoundDeltaSemantic},
		QualityOutcomes:   map[string]string{"call-mixed": ModelRoutingQualityReviewFixRequired},
	}})

	owner := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryNew, "HIGH", RoundDeltaSemantic, "opus", "glm-5.3")
	helper := routingCellOf(t, report.Cells, "worker", WorkerPhaseCategoryNew, "HIGH", RoundDeltaSemantic, "opus", "helper-model")
	if owner.TopLevelCalls != 1 || owner.Outcomes["success"] != 1 || owner.PacketStatuses["IMPLEMENTED"] != 1 {
		t.Fatalf("owner cell = %#v", owner)
	}
	if helper.TopLevelCalls != 0 || len(helper.Outcomes) != 0 || len(helper.PacketStatuses) != 0 {
		t.Fatalf("nested model inherited top-level verdict: %#v", helper)
	}
	if len(report.QualityGroups) != 1 || report.QualityGroups[0].ResolvedModel != "glm-5.3" ||
		report.QualityGroups[0].Outcomes[ModelRoutingQualityReviewFixRequired] != 1 {
		t.Fatalf("quality attribution = %#v", report.QualityGroups)
	}
}

func TestBuildModelRoutingReportUnknownConfoundersNeverComparable(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	logs := append(
		routingSufficientFixture("eeeeeeee", "opus", "glm-5.3", "", RoundDeltaUnknown, base),
		routingSufficientFixture("ffffffff", "haiku", "glm-4.7", "", RoundDeltaUnknown, base)...)
	report := BuildModelRoutingReport(logs)
	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaInsufficient || len(report.Evaluation.ComparableGroups) != 0 {
		t.Fatalf("unknown confounders became comparable: %#v", report.Evaluation)
	}
	for _, group := range report.QualityGroups {
		if group.Sufficient {
			t.Fatalf("unknown confounder group is sufficient: %#v", group)
		}
	}
}

func TestBuildModelRoutingReportOperationalFailuresDoNotSatisfyQuality(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	logs := append(
		routingSufficientFixture("12121212", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base),
		routingSufficientFixture("34343434", "haiku", "glm-4.7", "HIGH", RoundDeltaSemantic, base)...)
	for i := range logs {
		logs[i].QualityOutcomes = nil
		for j := range logs[i].Logs {
			logs[i].Logs[j].Outcome = "rate_limited"
			logs[i].Logs[j].PacketStatus = ""
		}
	}
	report := BuildModelRoutingReport(logs)
	if len(report.QualityGroups) != 0 || report.Evaluation.QualityDelta != ModelRoutingQualityDeltaUnknown {
		t.Fatalf("operational failures counted as quality evidence: %#v", report)
	}
}

func TestBuildModelRoutingReportQualitySufficiencyAggregatesAcrossAliases(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	left := routingSufficientFixture("56565656", "opus", "glm-5.3", "HIGH", RoundDeltaSemantic, base)
	for i := range left {
		for j := range left[i].Logs {
			if (i+j)%2 == 0 {
				left[i].Logs[j].ModelAlias = "sonnet"
			}
		}
	}
	left = append(left, routingSufficientFixture("78787878", "haiku", "glm-4.7", "HIGH", RoundDeltaSemantic, base)...)
	report := BuildModelRoutingReport(left)
	if report.Evaluation.QualityDelta != ModelRoutingQualityDeltaComparable {
		t.Fatalf("alias split changed quality aggregation: %#v", report.Evaluation)
	}
	groups := 0
	for _, group := range report.QualityGroups {
		if group.ResolvedModel == "glm-5.3" {
			groups++
			if group.Calls != ModelRoutingMinQualityCallsPerGroup || !group.Sufficient {
				t.Fatalf("alias-independent quality group = %#v", group)
			}
		}
	}
	if groups != 1 {
		t.Fatalf("glm-5.3 quality groups = %d: %#v", groups, report.QualityGroups)
	}
}
