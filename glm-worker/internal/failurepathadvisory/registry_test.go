package failurepathadvisory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func advisoryRegistryPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), RegistryFile)
}

func observedRecord(taskID string, findings ...Finding) Record {
	record := Record{TaskID: taskID, ReviewNumber: 1, Outcome: OutcomeObserved, Findings: findings}
	visible := 0
	for index := range record.Findings {
		if record.Findings[index].Status != FindingStatusVerified {
			continue
		}
		visibleIndex := visible
		record.Findings[index].VisibleIndex = &visibleIndex
		visible++
	}
	if visible > 0 {
		record.Advisory = &AdvisoryOutcome{Status: AdvisoryShown, FindingsShown: visible}
	}
	return record
}

func verifiedFinding(target string) Finding {
	return Finding{Target: target, Class: ClassExternalModelInvocation, Issue: "i", Status: FindingStatusVerified}
}

func TestRegistryRoundTripKeepsCohortOutcomes(t *testing.T) {
	path := advisoryRegistryPath(t)
	registry := Registry{}.WithRecord(observedRecord("task-a")).
		WithRecord(Record{TaskID: "task-b", Outcome: OutcomeAmbiguous}).
		WithRecord(Record{TaskID: "task-c", Outcome: OutcomeMissingDeadline}).
		WithRecord(Record{TaskID: "task-d", Outcome: OutcomeCapped})
	if err := SaveRegistry(path, registry); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CohortSize() != 2 {
		t.Fatalf("cohort size = %d want 2", loaded.CohortSize())
	}
	if !loaded.HasTaskRecord("task-b") || loaded.HasTaskRecord("task-e") {
		t.Fatal("task record検索が不正です")
	}
}

func TestParseStructuredOutputDistinguishesFindingStatus(t *testing.T) {
	raw := `{"findings":[
		{"target":"glm-worker/internal/runner/probe.go:80","class":"external-model-invocation","issue":"deadlineなし","evidence":"diff","status":"finding"},
		{"target":"glm-worker/internal/state/stats.go:9","class":"metric-reduction-accounting","issue":"欠測を成功へ算入","status":"indeterminate"}
	],"summary":"s"}`
	findings, err := ParseStructuredOutput([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Status != FindingStatusVerified || findings[1].Status != FindingStatusIndeterminate {
		t.Fatalf("status区分が保持されていません: %+v", findings)
	}
	empty, err := ParseStructuredOutput([]byte(`{"findings":[],"summary":"s"}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty findings = %+v err = %v", empty, err)
	}
}

func TestParseStructuredOutputRejectsInvalidPayloads(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"null":           "null",
		"unknown-field":  `{"findings":[],"summary":"s","extra":1}`,
		"bad-class":      `{"findings":[{"target":"a:1","class":"other","issue":"i","status":"finding"}],"summary":"s"}`,
		"missing-target": `{"findings":[{"class":"external-model-invocation","issue":"i","status":"finding"}],"summary":"s"}`,
		"empty-issue":    `{"findings":[{"target":"a:1","class":"external-model-invocation","issue":"","status":"finding"}],"summary":"s"}`,
		"missing-status": `{"findings":[{"target":"a:1","class":"external-model-invocation","issue":"i"}],"summary":"s"}`,
		"bad-status":     `{"findings":[{"target":"a:1","class":"external-model-invocation","issue":"i","status":"maybe"}],"summary":"s"}`,
	}
	for name, raw := range cases {
		if _, err := ParseStructuredOutput([]byte(raw)); err == nil {
			t.Fatalf("%sでerrorが返りませんでした", name)
		}
	}
	var many []map[string]any
	for index := 0; index <= findingsMaxItems; index++ {
		many = append(many, map[string]any{"target": "a:1", "class": ClassExternalOutputPersistence, "issue": "i", "status": FindingStatusVerified})
	}
	raw, err := json.Marshal(map[string]any{"findings": many, "summary": "s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseStructuredOutput(raw); err == nil {
		t.Fatal("findings上限超過でerrorが返りませんでした")
	}
}

func TestApplyLabelsValidatesAndStoresDispositions(t *testing.T) {
	registry := Registry{}.WithRecord(observedRecord("task-a", verifiedFinding("a:1"), verifiedFinding("a:2")))
	labels := LabelInput{
		Schema:       LabelsSchema,
		TaskID:       "task-a",
		ReviewNumber: 1,
		FindingDispositions: []FindingDispositionInput{
			{Index: 0, Disposition: DispositionTruePositive},
			{Index: 1, Disposition: DispositionFalsePositive},
		},
		AvoidedReviewFixWaves: 1,
		Usage:                 &UsageComparison{Measured: true, CodexTokens: 100},
	}
	updated, err := ApplyLabels(registry, labels)
	if err != nil {
		t.Fatal(err)
	}
	record := updated.Records[0]
	if record.Findings[0].Label.Disposition != DispositionTruePositive {
		t.Fatalf("label = %+v", record.Findings[0].Label)
	}
	if record.Assessment == nil || record.Assessment.AvoidedReviewFixWaves != 1 || !record.Assessment.Usage.Measured {
		t.Fatalf("assessment = %+v", record.Assessment)
	}
	reapplied, err := ApplyLabels(updated, labels)
	if err != nil {
		t.Fatal(err)
	}
	if len(reapplied.Records) != len(updated.Records) ||
		reapplied.Records[0].Findings[0].Label.Disposition != DispositionTruePositive ||
		reapplied.Records[0].Assessment.AvoidedReviewFixWaves != 1 {
		t.Fatalf("labels再適用でstateが変わりました: %+v", reapplied.Records[0])
	}
}

func TestApplyLabelsRejectsInvalidInput(t *testing.T) {
	registry := Registry{}.WithRecord(observedRecord("task-a", verifiedFinding("a:1"))).
		WithRecord(Record{TaskID: "task-b", ReviewNumber: 1, Outcome: OutcomeMissingDeadline})
	negative := -1
	cases := map[string]LabelInput{
		"unknown-task": {Schema: LabelsSchema, TaskID: "task-zz", ReviewNumber: 1},
		"non-observed": {Schema: LabelsSchema, TaskID: "task-b", ReviewNumber: 1},
		"missing-review": {Schema: LabelsSchema, TaskID: "task-a"},
		"index-range": {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1,
			FindingDispositions: []FindingDispositionInput{{Index: 5, Disposition: DispositionTruePositive}}},
		"bad-disposition": {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1,
			FindingDispositions: []FindingDispositionInput{{Index: 0, Disposition: "maybe"}}},
		"duplicate-index": {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1,
			FindingDispositions: []FindingDispositionInput{
				{Index: 0, Disposition: DispositionTruePositive},
				{Index: 0, Disposition: DispositionFalsePositive},
			}},
		"negative-false-negatives":     {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1, FalseNegatives: &negative},
		"negative-escaped-findings":    {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1, EscapedFindings: &negative},
		"negative-human-interventions": {Schema: LabelsSchema, TaskID: "task-a", ReviewNumber: 1, HumanInterventions: &negative},
		"bad-schema":                   {Schema: "other", TaskID: "task-a", ReviewNumber: 1},
	}
	for name, input := range cases {
		if _, err := ApplyLabels(registry, input); err == nil {
			t.Fatalf("%sでerrorが返りませんでした", name)
		}
	}
}

func TestApplyLabelsStoresAssessmentCounts(t *testing.T) {
	zero, two := 0, 2
	registry := Registry{}.WithRecord(observedRecord("task-a"))
	updated, err := ApplyLabels(registry, LabelInput{
		Schema:             LabelsSchema,
		TaskID:             "task-a",
		ReviewNumber:       1,
		FalseNegatives:     &zero,
		HumanInterventions: &two,
	})
	if err != nil {
		t.Fatal(err)
	}
	assessment := updated.Records[0].Assessment
	if assessment.FalseNegatives == nil || *assessment.FalseNegatives != 0 {
		t.Fatalf("false_negatives = %+v want measured zero", assessment.FalseNegatives)
	}
	if assessment.EscapedFindings != nil {
		t.Fatalf("escaped_findings = %+v want unknown", assessment.EscapedFindings)
	}
	if assessment.HumanInterventions == nil || *assessment.HumanInterventions != 2 {
		t.Fatalf("human_interventions = %+v want 2", assessment.HumanInterventions)
	}
}

func TestLoadLabelsParsesAssessmentCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.json")
	body := `{"schema":"` + LabelsSchema + `","task_id":"task-a","review_number":1,"finding_dispositions":[],` +
		`"false_negatives":0,"escaped_findings":1}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	labels, err := LoadLabels(path)
	if err != nil {
		t.Fatal(err)
	}
	if labels.ReviewNumber != 1 {
		t.Fatalf("review_number = %d want 1", labels.ReviewNumber)
	}
	if labels.FalseNegatives == nil || *labels.FalseNegatives != 0 {
		t.Fatalf("false_negatives = %+v want measured zero", labels.FalseNegatives)
	}
	if labels.EscapedFindings == nil || *labels.EscapedFindings != 1 {
		t.Fatalf("escaped_findings = %+v want 1", labels.EscapedFindings)
	}
	if labels.HumanInterventions != nil {
		t.Fatalf("human_interventions = %+v want unknown", labels.HumanInterventions)
	}
	negative := strings.Replace(body, `"escaped_findings":1`, `"escaped_findings":-1`, 1)
	if err := os.WriteFile(path, []byte(negative), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLabels(path); err == nil {
		t.Fatal("負値countでerrorが返りませんでした")
	}
}

func TestBuildSummaryEarlyStopEligibility(t *testing.T) {
	registry := Registry{}
	for index := 0; index < EarlyStopMinimumCohort; index++ {
		registry = registry.WithRecord(observedRecord("observed-" + string(rune('a'+index))))
	}
	if summary := BuildSummary(registry); !summary.EarlyStopEligible || summary.TruePositiveAdversarialOnly != 0 {
		t.Fatalf("findingなしcohortで早期停止可能になりません: %+v", summary)
	}

	withTruePositive := registry.WithRecord(observedRecord("task-tp",
		Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i", Status: FindingStatusVerified, Label: &FindingLabel{Disposition: DispositionTruePositive}}))
	if summary := BuildSummary(withTruePositive); summary.EarlyStopEligible {
		t.Fatal("真陽性1件で早期停止可能になりました")
	}

	unlabeled := registry.WithRecord(observedRecord("task-unlabeled", verifiedFinding("a:1")))
	if summary := BuildSummary(unlabeled); summary.EarlyStopEligible || summary.UnlabeledFindings != 1 {
		t.Fatalf("未label findingで早期停止可能になりました: %+v", summary)
	}

	small := Registry{}
	for index := 0; index < EarlyStopMinimumCohort-1; index++ {
		small = small.WithRecord(observedRecord("small-" + string(rune('a'+index))))
	}
	if summary := BuildSummary(small); summary.EarlyStopEligible {
		t.Fatal("cohort不足で早期停止可能になりました")
	}
}

func TestBuildSummaryAggregatesAddedGLMAndUsagePending(t *testing.T) {
	registry := Registry{}
	for index := 0; index < 3; index++ {
		record := observedRecord("task-" + string(rune('a'+index)))
		record.AddedGLM = &AddedGLMUsage{Calls: 1, InputTokens: 100, OutputTokens: 40, TotalCostUSD: 0.5, WallDurationMS: 900}
		registry = registry.WithRecord(record)
	}
	measured := observedRecord("task-measured")
	measured.AddedGLM = &AddedGLMUsage{Calls: 1}
	measured.Assessment = &SolAssessment{Usage: &UsageComparison{Measured: true}}
	registry = registry.WithRecord(measured)

	summary := BuildSummary(registry)
	if summary.AddedGLM.Calls != 4 || summary.AddedGLM.InputTokens != 300 || summary.AddedGLM.OutputTokens != 120 {
		t.Fatalf("added GLM = %+v", summary.AddedGLM)
	}
	if summary.UsageComparison.MeasuredTasks != 1 || summary.UsageComparison.PendingTasks != 3 {
		t.Fatalf("usage comparison = %+v", summary.UsageComparison)
	}
	if summary.CohortOpen != true || summary.CohortCap != CohortCap {
		t.Fatalf("cohort status = %+v", summary)
	}
}

func TestBuildSummarySeparatesMeasuredCountsFromUnknown(t *testing.T) {
	zero, three := 0, 3
	measuredZero := observedRecord("task-zero")
	measuredZero.Assessment = &SolAssessment{FalseNegatives: &zero, HumanInterventions: &zero}
	measured := observedRecord("task-measured", verifiedFinding("a:1"))
	measured.Assessment = &SolAssessment{FalseNegatives: &three}
	unknown := observedRecord("task-unknown", verifiedFinding("a:2"))
	registry := Registry{}.WithRecord(measuredZero).WithRecord(measured).WithRecord(unknown)

	summary := BuildSummary(registry)
	if summary.FalseNegatives.MeasuredTasks != 2 || summary.FalseNegatives.Total != 3 {
		t.Fatalf("false_negatives = %+v", summary.FalseNegatives)
	}
	if summary.HumanInterventions.MeasuredTasks != 1 || summary.HumanInterventions.Total != 0 {
		t.Fatalf("human_interventions = %+v want measured zero 1task", summary.HumanInterventions)
	}
	if summary.EscapedFindings.MeasuredTasks != 0 || summary.EscapedFindings.Total != 0 {
		t.Fatalf("escaped_findings = %+v want all unknown", summary.EscapedFindings)
	}
}

func TestBuildSummaryTreatsLegacyRecordsAsUnknownCounts(t *testing.T) {
	path := advisoryRegistryPath(t)
	legacy := `{"schema":"` + AdvisorySchema + `","records":[` +
		`{"task_id":"legacy-task","outcome":"observed","findings":[` +
		`{"target":"a:1","class":"external-model-invocation","issue":"i","status":"finding",` +
		`"label":{"disposition":"true-positive"}}],` +
		`"sol_assessment":{"avoided_review_fix_waves":1,"quality_delta_note":"旧形式"}}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	summary := BuildSummary(registry)
	if summary.TruePositiveAdversarialOnly != 1 {
		t.Fatalf("legacy recordの既存集計が変わりました: %+v", summary)
	}
	if summary.FalseNegatives.MeasuredTasks != 0 || summary.EscapedFindings.MeasuredTasks != 0 ||
		summary.HumanInterventions.MeasuredTasks != 0 {
		t.Fatalf("legacy recordがmeasured count扱いになりました: %+v", summary)
	}
}

func TestBuildSummaryCountsAdvisoryOutcomesAndIndeterminate(t *testing.T) {
	shown := observedRecord("task-shown", verifiedFinding("a:1"),
		Finding{Target: "a:2", Class: ClassMetricReductionAccounting, Issue: "不明", Status: FindingStatusIndeterminate})
	shown.Advisory = &AdvisoryOutcome{Status: AdvisoryShown, FindingsShown: 1, Indeterminate: 1}
	failOpen := observedRecord("task-fail-open")
	failOpen.Advisory = &AdvisoryOutcome{Status: AdvisoryOmittedFailOpen}
	registry := Registry{}.WithRecord(shown).WithRecord(failOpen)

	summary := BuildSummary(registry)
	if summary.AdvisoryShownRecords != 1 || summary.AdvisoryOmittedCounts[AdvisoryOmittedFailOpen] != 1 {
		t.Fatalf("advisory counts = shown:%d omitted:%v", summary.AdvisoryShownRecords, summary.AdvisoryOmittedCounts)
	}
	if summary.FindingsTotal != 2 || summary.IndeterminateFindings != 1 {
		t.Fatalf("findings total = %d indeterminate = %d", summary.FindingsTotal, summary.IndeterminateFindings)
	}
}

func TestStructuredSchemaJSONPinsAdvisoryContract(t *testing.T) {
	schema, err := StructuredSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range Classes {
		if !strings.Contains(schema, class) {
			t.Fatalf("schemaにclass %sがありません", class)
		}
	}
	for _, status := range []string{FindingStatusVerified, FindingStatusIndeterminate} {
		if !strings.Contains(schema, status) {
			t.Fatalf("schemaにstatus %sがありません", status)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("schemaがJSONとして不正です: %v", err)
	}
}

func TestSaveRegistryRejectsWrongSchemaOnLoad(t *testing.T) {
	path := advisoryRegistryPath(t)
	if err := os.WriteFile(path, []byte(`{"schema":"other","records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("不正schema registryが読み込まれました")
	}
}
