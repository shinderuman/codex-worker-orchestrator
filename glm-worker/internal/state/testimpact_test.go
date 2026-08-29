package state

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const testImpactTaskA = "11111111-1111-4111-8111-111111111111"

const testImpactTaskB = "22222222-2222-4222-8222-222222222222"

const testImpactTaskC = "33333333-3333-4333-8333-333333333333"

func testImpactBaseTime() time.Time {
	return time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
}

func testImpactTaskARecords() []TaskEventRecord {
	base := testImpactBaseTime()
	return []TaskEventRecord{
		{
			TaskID: testImpactTaskA, CallID: "call-a", Role: "worker", Phase: "worker-new",
			Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []TaskBlockSummary{
				{Type: "tool_use", Name: "Bash", ToolID: "t1", OperationCategory: OperationCategoryTest, Bytes: 80},
				{Type: "tool_use", Name: "Edit", ToolID: "t2", OperationCategory: OperationCategoryFileWrite, Bytes: 60},
			},
		},
		{
			TaskID: testImpactTaskA, CallID: "call-a", Role: "worker", Phase: "worker-new",
			Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []TaskBlockSummary{
				{Type: "tool_result", Name: "Bash", ToolID: "t1", OperationCategory: OperationCategoryTest, Bytes: 100, DurationMS: 1500},
				{Type: "tool_result", Name: "Edit", ToolID: "t2", OperationCategory: OperationCategoryFileWrite, Bytes: 90},
			},
		},
	}
}

func testImpactTaskBRecords() []TaskEventRecord {
	base := testImpactBaseTime().Add(time.Hour)
	return []TaskEventRecord{
		{
			TaskID: testImpactTaskB, CallID: "call-b", Role: "worker", Phase: "worker-explicit-fix",
			Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []TaskBlockSummary{
				{Type: "tool_use", Name: "Bash", ToolID: "s1", OperationCategory: OperationCategoryTest, Bytes: 80},
				{Type: "tool_use", Name: "Bash", ToolID: "s2", OperationCategory: OperationCategoryTest, Bytes: 81},
				{Type: "tool_use", Name: "Write", ToolID: "s3", OperationCategory: OperationCategoryFileWrite, Bytes: 60},
			},
		},
		{
			TaskID: testImpactTaskB, CallID: "call-b", Role: "worker", Phase: "worker-explicit-fix",
			Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []TaskBlockSummary{
				{Type: "tool_result", Name: "Bash", ToolID: "s1", OperationCategory: OperationCategoryTest, Bytes: 100, DurationMS: 2000, IsError: true},
				{Type: "tool_result", Name: "Bash", ToolID: "s2", OperationCategory: OperationCategoryTest, Bytes: 101},
				{Type: "tool_result", Name: "Write", ToolID: "s3", OperationCategory: OperationCategoryFileWrite, Bytes: 90, DurationMS: 100},
			},
		},
	}
}

func testImpactTaskCRecords() []TaskEventRecord {
	base := testImpactBaseTime().Add(2 * time.Hour)
	return []TaskEventRecord{
		{
			TaskID: testImpactTaskC, CallID: "call-c", Role: "worker", Phase: "worker-new",
			Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []TaskBlockSummary{
				{Type: "tool_use", Name: "Edit", ToolID: "f1", OperationCategory: OperationCategoryFileWrite, Bytes: 60},
			},
		},
		{
			TaskID: testImpactTaskC, CallID: "call-c", Role: "worker", Phase: "worker-new",
			Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []TaskBlockSummary{
				{Type: "tool_result", Name: "Edit", ToolID: "f1", OperationCategory: OperationCategoryFileWrite, Bytes: 90, DurationMS: 50},
			},
		},
	}
}

func testImpactFixtureTasks() []TaskEvents {
	return []TaskEvents{
		{TaskID: testImpactTaskB, Records: testImpactTaskBRecords()},
		{TaskID: testImpactTaskC, Records: testImpactTaskCRecords()},
		{TaskID: testImpactTaskA, Records: testImpactTaskARecords()},
	}
}

func testImpactFixtureReviews() map[string]TestImpactReviewSummary {
	return map[string]TestImpactReviewSummary{
		testImpactTaskA: {PassCalls: 1, Outcome: ModelRoutingQualityReviewPass},
		testImpactTaskC: {FixRequiredCalls: 1, Outcome: ModelRoutingQualityReviewFixRequired},
	}
}

func assertTestImpactMeasures(t *testing.T, got []TestImpactCategoryMeasure, want []TestImpactCategoryMeasure) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("measures = %#v, want %#v", got, want)
	}
}

func TestBuildTestImpactReportPerTaskOperationsAndTotals(t *testing.T) {
	report := BuildTestImpactReport(testImpactFixtureTasks(), testImpactFixtureReviews())

	if report.Retention != retainedTaskEventLogs {
		t.Fatalf("retention = %d", report.Retention)
	}
	if report.Sources != (TestImpactSources{
		EventLog:        testImpactEventLogSource,
		ReviewOutcome:   testImpactReviewOutcomeSource,
		WriteOperations: testImpactWriteOperationsSource,
	}) {
		t.Fatalf("sources = %#v", report.Sources)
	}
	if len(report.Tasks) != 3 {
		t.Fatalf("tasks = %#v", report.Tasks)
	}
	taskA, taskB, taskC := report.Tasks[0], report.Tasks[1], report.Tasks[2]
	if taskA.TaskID != testImpactTaskA || taskB.TaskID != testImpactTaskB || taskC.TaskID != testImpactTaskC {
		t.Fatalf("task順序 = %s %s %s", taskA.TaskID, taskB.TaskID, taskC.TaskID)
	}
	assertTestImpactMeasures(t, taskA.Operations, []TestImpactCategoryMeasure{
		{Category: OperationCategoryFileWrite, Uses: 1, Results: 1, Unmeasured: 1},
		{Category: OperationCategoryTest, Uses: 1, Results: 1, Measured: 1, MeasuredSumMS: 1500, MeasuredMaxMS: 1500},
	})
	assertTestImpactMeasures(t, taskB.Operations, []TestImpactCategoryMeasure{
		{Category: OperationCategoryFileWrite, Uses: 1, Results: 1, Measured: 1, MeasuredSumMS: 100, MeasuredMaxMS: 100},
		{Category: OperationCategoryTest, Uses: 2, Results: 2, Measured: 1, MeasuredSumMS: 2000, MeasuredMaxMS: 2000, Unmeasured: 1, Errors: 1},
	})
	assertTestImpactMeasures(t, taskC.Operations, []TestImpactCategoryMeasure{
		{Category: OperationCategoryFileWrite, Uses: 1, Results: 1, Measured: 1, MeasuredSumMS: 50, MeasuredMaxMS: 50},
	})
	assertTestImpactMeasures(t, report.CategoryTotals, []TestImpactCategoryMeasure{
		{Category: OperationCategoryFileWrite, Uses: 3, Results: 3, Measured: 2, MeasuredSumMS: 150, MeasuredMaxMS: 100, Unmeasured: 1},
		{Category: OperationCategoryTest, Uses: 3, Results: 3, Measured: 2, MeasuredSumMS: 3500, MeasuredMaxMS: 2000, Unmeasured: 1, Errors: 1},
	})
	if taskA.Review.Outcome != ModelRoutingQualityReviewPass || taskA.Review.PassCalls != 1 {
		t.Fatalf("taskA review = %#v", taskA.Review)
	}
	if taskB.Review.Outcome != TestImpactReviewOutcomeUnknown {
		t.Fatalf("taskB review = %#v", taskB.Review)
	}
	if taskC.Review.Outcome != ModelRoutingQualityReviewFixRequired || taskC.Review.FixRequiredCalls != 1 {
		t.Fatalf("taskC review = %#v", taskC.Review)
	}
}

func TestBuildTestImpactReportSkipsEmptyTaskRecords(t *testing.T) {
	report := BuildTestImpactReport([]TaskEvents{{TaskID: testImpactTaskA}}, nil)
	if len(report.Tasks) != 0 || len(report.CategoryTotals) != 0 {
		t.Fatalf("空records taskが集計されました: %#v", report)
	}
}

func TestBuildTestImpactReportEvaluationReasons(t *testing.T) {
	cases := []struct {
		name         string
		tasks        []TaskEvents
		reviews      map[string]TestImpactReviewSummary
		wantTasks    int
		wantTotals   int
		wantContains []string
	}{
		{
			name:         "empty events",
			wantTasks:    0,
			wantTotals:   0,
			wantContains: []string{"suite-level coverage is unknown", "no task event logs are retained"},
		},
		{
			name:       "no recorded errors",
			tasks:      []TaskEvents{{TaskID: testImpactTaskA, Records: testImpactTaskARecords()}},
			wantTasks:  1,
			wantTotals: 2,
			wantContains: []string{
				"suite-level coverage is unknown",
				"most recent 10 past task event logs",
				"1 test-category tool call",
				"no test-category tool error",
				"0 of 1 tasks",
				"no attributable review outcome",
				"separate blocked decision",
			},
		},
		{
			name:       "recorded errors",
			tasks:      testImpactFixtureTasks(),
			reviews:    testImpactFixtureReviews(),
			wantTasks:  3,
			wantTotals: 2,
			wantContains: []string{
				"suite-level coverage is unknown",
				"most recent 10 past task event logs",
				"3 test-category tool call",
				"1 test-category tool error",
				"1 of 3 tasks",
				"no attributable review outcome",
				"separate blocked decision",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := BuildTestImpactReport(tc.tasks, tc.reviews)

			if len(report.Tasks) != tc.wantTasks || len(report.CategoryTotals) != tc.wantTotals {
				t.Fatalf("tasks/totals = %d/%d, want %d/%d", len(report.Tasks), len(report.CategoryTotals), tc.wantTasks, tc.wantTotals)
			}
			if report.Retention != retainedTaskEventLogs {
				t.Fatalf("retention = %d", report.Retention)
			}
			if report.Evaluation.SuiteCoverage != TestImpactSuiteCoverageUnknown {
				t.Fatalf("suite coverage = %#v", report.Evaluation.SuiteCoverage)
			}
			if len(report.Evaluation.OmissionCandidates) != 0 {
				t.Fatalf("omission candidates = %#v", report.Evaluation.OmissionCandidates)
			}
			joined := strings.Join(report.Evaluation.Reasons, "\n")
			for _, phrase := range tc.wantContains {
				if !strings.Contains(joined, phrase) {
					t.Fatalf("reasons中に%qがありません: %#v", phrase, report.Evaluation.Reasons)
				}
			}
		})
	}
}

func TestTestImpactReviewFromCallOutcomes(t *testing.T) {
	empty := TestImpactReviewFromCallOutcomes(map[string]string{})
	if empty.Outcome != TestImpactReviewOutcomeUnknown || empty.PassCalls != 0 || empty.FixRequiredCalls != 0 {
		t.Fatalf("空outcomes = %#v", empty)
	}
	passOnly := TestImpactReviewFromCallOutcomes(map[string]string{
		"call-1": ModelRoutingQualityReviewPass,
		"call-2": ModelRoutingQualityReviewPass,
	})
	if passOnly.Outcome != ModelRoutingQualityReviewPass || passOnly.PassCalls != 2 {
		t.Fatalf("passのみ = %#v", passOnly)
	}
	mixed := TestImpactReviewFromCallOutcomes(map[string]string{
		"call-1": ModelRoutingQualityReviewPass,
		"call-2": ModelRoutingQualityReviewFixRequired,
		"call-3": "unattributable",
	})
	if mixed.Outcome != ModelRoutingQualityReviewFixRequired || mixed.PassCalls != 1 || mixed.FixRequiredCalls != 1 {
		t.Fatalf("mixed = %#v", mixed)
	}
}
