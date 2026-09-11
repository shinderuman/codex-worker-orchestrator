package state

import (
	"fmt"
	"slices"
	"strings"
)

type TestImpactCategoryMeasure struct {
	Category      string `json:"category"`
	Uses          int    `json:"uses"`
	Results       int    `json:"results,omitempty"`
	Measured      int    `json:"measured,omitempty"`
	MeasuredSumMS int64  `json:"measured_sum_ms,omitempty"`
	MeasuredMaxMS int64  `json:"measured_max_ms,omitempty"`
	Unmeasured    int    `json:"unmeasured,omitempty"`
	Errors        int    `json:"errors,omitempty"`
}

type TestImpactReviewSummary struct {
	PassCalls        int    `json:"pass_calls,omitempty"`
	FixRequiredCalls int    `json:"fix_required_calls,omitempty"`
	Outcome          string `json:"outcome"`
}

type TestImpactTaskSummary struct {
	TaskID      string                        `json:"task_id"`
	Operations  []TestImpactCategoryMeasure   `json:"operations"`
	Validations []TestImpactValidationMeasure `json:"validations,omitempty"`
	Review      TestImpactReviewSummary       `json:"review"`
}

type TestImpactEvaluation struct {
	SuiteCoverage            string   `json:"suite_coverage"`
	StructuredValidationRuns int      `json:"structured_validation_runs,omitempty"`
	UnknownValidationRuns    int      `json:"unknown_validation_runs,omitempty"`
	OmissionCandidates       []string `json:"omission_candidates"`
	Reasons                  []string `json:"reasons"`
}

type TestImpactSources struct {
	EventLog        string `json:"event_log"`
	ReviewOutcome   string `json:"review_outcome"`
	WriteOperations string `json:"write_operations"`
}

type TestImpactReport struct {
	Sources          TestImpactSources              `json:"sources"`
	Retention        int                            `json:"retention"`
	Tasks            []TestImpactTaskSummary        `json:"tasks"`
	CategoryTotals   []TestImpactCategoryMeasure    `json:"category_totals"`
	ValidationTotals []TestImpactValidationMeasure  `json:"validation_totals,omitempty"`
	Evaluation       TestImpactEvaluation           `json:"evaluation"`
}

const (
	TestImpactSuiteCoverageUnknown = "unknown"
	TestImpactSuiteCoveragePartial = "partial"
	TestImpactReviewOutcomeUnknown = "unknown"
)

const testImpactEventLogSource = "task event logs (events/<task-id>.jsonl) retain bounded structured validation observations from execution points alongside the closed operation_category; known suite/class/result/duration/phase/snapshot/attempt fields are primary validation evidence while raw commands, arguments, stdout and stderr are not stored"

const testImpactReviewOutcomeSource = "per-task escaped signal reuses the existing deterministic review outcome attribution: worker calls with an implemented packet reviewed PASS or FIX_REQUIRED from saved ModelCallLog v3 and RoundRecord; tasks without attributable review outcome stay unknown"

const testImpactWriteOperationsSource = "write operations are the existing deterministic file-write and git-write categories; no new change classification or AI classification is introduced"

const testImpactSuiteCoverageReason = "no structured validation records are retained, so suite-level coverage is unknown; legacy operation categories alone are not promoted to suite evidence"

const testImpactSuiteCoveragePartialReason = "structured validation records identify known suites at execution time, but retained history can still contain legacy or unknown observations; coverage is partial rather than inferred complete"

const testImpactOmissionReason = "no omission candidate is presented: omission requires per-suite failure and escaped-defect contrast that these deterministic sources cannot attribute, and test selection introduction stays a separate blocked decision"

const testImpactNoFailureReason = "no test-category tool errors are recorded in the retained window; failure-free execution alone is not quality evidence for omitting any test"

const testImpactFoldingReason = "legacy operation_category can still classify compound shell execution as other; structured validation observations are therefore the primary suite evidence and operation-category test counts remain auxiliary"

const testImpactEmptyEventsReason = "no task event logs are retained; test call counts, durations, and failure outcomes are unavailable"

var testImpactWriteCategories = []string{OperationCategoryFileWrite, OperationCategoryGitWrite}

func BuildTestImpactReport(tasks []TaskEvents, reviews map[string]TestImpactReviewSummary) TestImpactReport {
	report := TestImpactReport{
		Sources: TestImpactSources{
			EventLog:        testImpactEventLogSource,
			ReviewOutcome:   testImpactReviewOutcomeSource,
			WriteOperations: testImpactWriteOperationsSource,
		},
		Retention:      retainedTaskEventLogs,
		Tasks:            []TestImpactTaskSummary{},
		CategoryTotals:   []TestImpactCategoryMeasure{},
		ValidationTotals: []TestImpactValidationMeasure{},
		Evaluation: TestImpactEvaluation{
			SuiteCoverage:      TestImpactSuiteCoverageUnknown,
			OmissionCandidates: []string{},
			Reasons:            []string{},
		},
	}
	totals := make(map[string]*TestImpactCategoryMeasure)
	validationTotals := make(map[string]*TestImpactValidationMeasure)
	for _, task := range sortedTestImpactTasks(tasks) {
		if len(task.Records) == 0 {
			continue
		}
		operations := testImpactCategoryMeasures(task.Records)
		for index := range operations {
			absorbTestImpactMeasure(totals, operations[index])
		}
		validations := testImpactValidationMeasures(task)
		for index := range validations {
			absorbTestImpactValidationMeasure(validationTotals, validations[index])
			report.Evaluation.StructuredValidationRuns += validations[index].Runs
			report.Evaluation.UnknownValidationRuns += validations[index].Unknown
			if validations[index].GateClass == ValidationGateClassUnknown {
				report.Evaluation.UnknownValidationRuns += validations[index].Runs - validations[index].Unknown
			}
		}
		review := reviews[task.TaskID]
		if review.Outcome == "" {
			review.Outcome = TestImpactReviewOutcomeUnknown
		}
		report.Tasks = append(report.Tasks, TestImpactTaskSummary{
			TaskID:      task.TaskID,
			Operations:  operations,
			Validations: validations,
			Review:      review,
		})
	}
	report.CategoryTotals = sortedTestImpactMeasures(totals)
	report.ValidationTotals = sortedTestImpactValidationMeasures(validationTotals)
	if report.Evaluation.StructuredValidationRuns > 0 {
		report.Evaluation.SuiteCoverage = TestImpactSuiteCoveragePartial
	}
	report.Evaluation.Reasons = testImpactReasons(report)
	return report
}

func TestImpactReviewFromCallOutcomes(outcomes map[string]string) TestImpactReviewSummary {
	summary := TestImpactReviewSummary{Outcome: TestImpactReviewOutcomeUnknown}
	for _, outcome := range outcomes {
		switch outcome {
		case ModelRoutingQualityReviewFixRequired:
			summary.FixRequiredCalls++
		case ModelRoutingQualityReviewPass:
			summary.PassCalls++
		}
	}
	if summary.FixRequiredCalls > 0 {
		summary.Outcome = ModelRoutingQualityReviewFixRequired
		return summary
	}
	if summary.PassCalls > 0 {
		summary.Outcome = ModelRoutingQualityReviewPass
	}
	return summary
}

func sortedTestImpactTasks(tasks []TaskEvents) []TaskEvents {
	sorted := make([]TaskEvents, len(tasks))
	copy(sorted, tasks)
	slices.SortFunc(sorted, func(a, b TaskEvents) int {
		return strings.Compare(a.TaskID, b.TaskID)
	})
	return sorted
}

func testImpactCategoryMeasures(records []TaskEventRecord) []TestImpactCategoryMeasure {
	operations := SumCallTimelineOperations(CallsFromTaskEvents(records))
	measures := make([]TestImpactCategoryMeasure, 0, len(operations))
	for _, operation := range operations {
		measures = append(measures, TestImpactCategoryMeasure{
			Category:      operation.Category,
			Uses:          operation.Uses,
			Results:       operation.Results,
			Measured:      operation.Measured,
			MeasuredSumMS: operation.MeasuredSumMS,
			MeasuredMaxMS: operation.MeasuredMaxMS,
			Unmeasured:    operation.Unmeasured,
			Errors:        operation.Errors,
		})
	}
	return measures
}

func absorbTestImpactMeasure(totals map[string]*TestImpactCategoryMeasure, measure TestImpactCategoryMeasure) {
	total := totals[measure.Category]
	if total == nil {
		total = &TestImpactCategoryMeasure{Category: measure.Category}
		totals[measure.Category] = total
	}
	total.Uses += measure.Uses
	total.Results += measure.Results
	total.Measured += measure.Measured
	total.MeasuredSumMS += measure.MeasuredSumMS
	if measure.MeasuredMaxMS > total.MeasuredMaxMS {
		total.MeasuredMaxMS = measure.MeasuredMaxMS
	}
	total.Unmeasured += measure.Unmeasured
	total.Errors += measure.Errors
}

func sortedTestImpactMeasures(totals map[string]*TestImpactCategoryMeasure) []TestImpactCategoryMeasure {
	measures := make([]TestImpactCategoryMeasure, 0, len(totals))
	for _, total := range totals {
		measures = append(measures, *total)
	}
	slices.SortFunc(measures, func(a, b TestImpactCategoryMeasure) int {
		return strings.Compare(a.Category, b.Category)
	})
	return measures
}

func testImpactReasons(report TestImpactReport) []string {
	reasons := []string{}
	if report.Evaluation.StructuredValidationRuns == 0 {
		reasons = append(reasons, testImpactSuiteCoverageReason)
	} else {
		reasons = append(reasons, testImpactSuiteCoveragePartialReason)
		reasons = append(reasons, fmt.Sprintf(
			"retained structured validation evidence contains %d runs with %d unknown result or gate-class runs",
			report.Evaluation.StructuredValidationRuns, report.Evaluation.UnknownValidationRuns))
	}
	if len(report.Tasks) == 0 {
		return append(reasons, testImpactEmptyEventsReason)
	}
	test := testImpactFindCategory(report.CategoryTotals, OperationCategoryTest)
	tasksWithTest := testImpactTasksWithCategory(report.Tasks, OperationCategoryTest)
	reasons = append(reasons, fmt.Sprintf(
		"event retention keeps the most recent %d past task event logs plus the current task, so aggregates cover only these %d retained tasks and earlier test activity is not observable",
		report.Retention, len(report.Tasks)))
	reasons = append(reasons, fmt.Sprintf(
		"retained events record %d test-category tool calls (%d results observed, %d measured, %d unmeasured, %d errors) across %d tasks",
		test.Uses, test.Results, test.Measured, test.Unmeasured, test.Errors, tasksWithTest))
	reasons = append(reasons, testImpactFoldingReason)
	if test.Errors == 0 {
		reasons = append(reasons, testImpactNoFailureReason)
	} else {
		reasons = append(reasons, fmt.Sprintf(
			"%d test-category tool errors are recorded; recorded failures document executed value and are not omission evidence", test.Errors))
	}
	writeTasks, writeTasksWithoutTest := testImpactWriteTaskCounts(report.Tasks)
	if writeTasks > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%d of %d tasks with file-write or git-write tool use recorded no test-category tool use",
			writeTasksWithoutTest, writeTasks))
	}
	unknownReview := 0
	for _, task := range report.Tasks {
		if task.Review.Outcome == TestImpactReviewOutcomeUnknown {
			unknownReview++
		}
	}
	if unknownReview > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%d tasks have no attributable review outcome and stay unknown", unknownReview))
	}
	return append(reasons, testImpactOmissionReason)
}

func testImpactFindCategory(measures []TestImpactCategoryMeasure, category string) TestImpactCategoryMeasure {
	for _, measure := range measures {
		if measure.Category == category {
			return measure
		}
	}
	return TestImpactCategoryMeasure{Category: category}
}

func testImpactTasksWithCategory(tasks []TestImpactTaskSummary, category string) int {
	count := 0
	for _, task := range tasks {
		measure := testImpactFindCategory(task.Operations, category)
		if measure.Uses > 0 {
			count++
		}
	}
	return count
}

func testImpactWriteTaskCounts(tasks []TestImpactTaskSummary) (int, int) {
	writeTasks := 0
	writeTasksWithoutTest := 0
	for _, task := range tasks {
		hasWrite := false
		for _, category := range testImpactWriteCategories {
			if testImpactFindCategory(task.Operations, category).Uses > 0 {
				hasWrite = true
				break
			}
		}
		if !hasWrite {
			continue
		}
		writeTasks++
		if testImpactFindCategory(task.Operations, OperationCategoryTest).Uses == 0 {
			writeTasksWithoutTest++
		}
	}
	return writeTasks, writeTasksWithoutTest
}
