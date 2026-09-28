package failurepathtrial

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func trialRegistryPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), RegistryFile)
}

func observedRecord(taskID string, findings ...Finding) Record {
	return Record{TaskID: taskID, Outcome: OutcomeObserved, Findings: findings}
}

func TestRegistryRoundTripKeepsCohortOutcomes(t *testing.T) {
	path := trialRegistryPath(t)
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

func TestParseShadowOutputAcceptsFindingsAndEmptyList(t *testing.T) {
	raw := `{"findings":[{"target":"glm-worker/internal/runner/probe.go:80","class":"external-model-invocation","issue":"deadlineなし","evidence":"diff"}],"summary":"s"}`
	findings, err := ParseShadowOutput([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Class != ClassExternalModelInvocation {
		t.Fatalf("findings = %+v", findings)
	}
	empty, err := ParseShadowOutput([]byte(`{"findings":[],"summary":"s"}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty findings = %+v err = %v", empty, err)
	}
}

func TestParseShadowOutputRejectsInvalidPayloads(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"null":           "null",
		"unknown-field":  `{"findings":[],"summary":"s","extra":1}`,
		"bad-class":      `{"findings":[{"target":"a:1","class":"other","issue":"i"}],"summary":"s"}`,
		"missing-target": `{"findings":[{"class":"external-model-invocation","issue":"i"}],"summary":"s"}`,
		"empty-issue":    `{"findings":[{"target":"a:1","class":"external-model-invocation","issue":""}],"summary":"s"}`,
	}
	for name, raw := range cases {
		if _, err := ParseShadowOutput([]byte(raw)); err == nil {
			t.Fatalf("%sでerrorが返りませんでした", name)
		}
	}
	var many []map[string]any
	for index := 0; index <= findingsMaxItems; index++ {
		many = append(many, map[string]any{"target": "a:1", "class": ClassExternalOutputPersistence, "issue": "i"})
	}
	raw, err := json.Marshal(map[string]any{"findings": many, "summary": "s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseShadowOutput(raw); err == nil {
		t.Fatal("findings上限超過でerrorが返りませんでした")
	}
}

func TestApplyLabelsValidatesAndStoresDispositions(t *testing.T) {
	registry := Registry{}.WithRecord(observedRecord("task-a",
		Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i1"},
		Finding{Target: "a:2", Class: ClassReviewLifecycleRouting, Issue: "i2"},
	))
	labels := LabelInput{
		Schema: LabelsSchema,
		TaskID: "task-a",
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
	registry := Registry{}.WithRecord(observedRecord("task-a", Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i"})).
		WithRecord(Record{TaskID: "task-b", Outcome: OutcomeMissingDeadline})
	cases := map[string]LabelInput{
		"unknown-task": {Schema: LabelsSchema, TaskID: "task-zz"},
		"non-observed": {Schema: LabelsSchema, TaskID: "task-b"},
		"index-range": {Schema: LabelsSchema, TaskID: "task-a",
			FindingDispositions: []FindingDispositionInput{{Index: 5, Disposition: DispositionTruePositive}}},
		"bad-disposition": {Schema: LabelsSchema, TaskID: "task-a",
			FindingDispositions: []FindingDispositionInput{{Index: 0, Disposition: "maybe"}}},
		"duplicate-index": {Schema: LabelsSchema, TaskID: "task-a",
			FindingDispositions: []FindingDispositionInput{
				{Index: 0, Disposition: DispositionTruePositive},
				{Index: 0, Disposition: DispositionFalsePositive},
			}},
		"bad-schema": {Schema: "other", TaskID: "task-a"},
	}
	for name, input := range cases {
		if _, err := ApplyLabels(registry, input); err == nil {
			t.Fatalf("%sでerrorが返りませんでした", name)
		}
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
		Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i", Label: &FindingLabel{Disposition: DispositionTruePositive}}))
	if summary := BuildSummary(withTruePositive); summary.EarlyStopEligible {
		t.Fatal("真陽性1件で早期停止可能になりました")
	}

	unlabeled := registry.WithRecord(observedRecord("task-unlabeled",
		Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i"}))
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

func TestShadowSchemaJSONPinsTrialContract(t *testing.T) {
	schema, err := ShadowSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range Classes {
		if !strings.Contains(schema, class) {
			t.Fatalf("schemaにclass %sがありません", class)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("schemaがJSONとして不正です: %v", err)
	}
}

func TestSaveRegistryRejectsWrongSchemaOnLoad(t *testing.T) {
	path := trialRegistryPath(t)
	if err := os.WriteFile(path, []byte(`{"schema":"other","records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("不正schema registryが読み込まれました")
	}
}
