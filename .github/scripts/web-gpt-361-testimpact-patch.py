from pathlib import Path

path = Path("glm-worker/internal/state/testimpact.go")
text = path.read_text()

replacements = [
(
'''type TestImpactTaskSummary struct {
\tTaskID     string                      `json:"task_id"`
\tOperations []TestImpactCategoryMeasure `json:"operations"`
\tReview     TestImpactReviewSummary     `json:"review"`
}
''',
'''type TestImpactTaskSummary struct {
\tTaskID      string                        `json:"task_id"`
\tOperations  []TestImpactCategoryMeasure   `json:"operations"`
\tValidations []TestImpactValidationMeasure `json:"validations,omitempty"`
\tReview      TestImpactReviewSummary       `json:"review"`
}
'''),
(
'''type TestImpactEvaluation struct {
\tSuiteCoverage      string   `json:"suite_coverage"`
\tOmissionCandidates []string `json:"omission_candidates"`
\tReasons            []string `json:"reasons"`
}
''',
'''type TestImpactEvaluation struct {
\tSuiteCoverage            string   `json:"suite_coverage"`
\tStructuredValidationRuns int      `json:"structured_validation_runs,omitempty"`
\tUnknownValidationRuns    int      `json:"unknown_validation_runs,omitempty"`
\tOmissionCandidates       []string `json:"omission_candidates"`
\tReasons                  []string `json:"reasons"`
}
'''),
(
'''type TestImpactReport struct {
\tSources        TestImpactSources           `json:"sources"`
\tRetention      int                         `json:"retention"`
\tTasks          []TestImpactTaskSummary     `json:"tasks"`
\tCategoryTotals []TestImpactCategoryMeasure `json:"category_totals"`
\tEvaluation     TestImpactEvaluation        `json:"evaluation"`
}
''',
'''type TestImpactReport struct {
\tSources          TestImpactSources              `json:"sources"`
\tRetention        int                            `json:"retention"`
\tTasks            []TestImpactTaskSummary        `json:"tasks"`
\tCategoryTotals   []TestImpactCategoryMeasure    `json:"category_totals"`
\tValidationTotals []TestImpactValidationMeasure  `json:"validation_totals,omitempty"`
\tEvaluation       TestImpactEvaluation           `json:"evaluation"`
}
'''),
(
'''const (
\tTestImpactSuiteCoverageUnknown = "unknown"
\tTestImpactReviewOutcomeUnknown = "unknown"
)
''',
'''const (
\tTestImpactSuiteCoverageUnknown = "unknown"
\tTestImpactSuiteCoveragePartial = "partial"
\tTestImpactReviewOutcomeUnknown = "unknown"
)
'''),
(
'''const testImpactEventLogSource = "task event logs (events/<task-id>.jsonl) attach the existing ten-value closed operation_category to tool_use/tool_result blocks with per-block duration_ms and is_error; raw commands, arguments, and suite identity are not saved, so test subtypes below the closed set and suite-level coverage stay unknown"
''',
'''const testImpactEventLogSource = "task event logs (events/<task-id>.jsonl) retain bounded structured validation observations from execution points alongside the closed operation_category; known suite/class/result/duration/phase/snapshot/attempt fields are primary validation evidence while raw commands, arguments, stdout and stderr are not stored"
'''),
(
'''const testImpactSuiteCoverageReason = "task event blocks save only the ten-value closed operation_category; raw commands, arguments, and suite identity are not recorded, so suite-level coverage is unknown and unit/race/vet/integration subtypes are not distinguishable"
''',
'''const testImpactSuiteCoverageReason = "no structured validation records are retained, so suite-level coverage is unknown; legacy operation categories alone are not promoted to suite evidence"

const testImpactSuiteCoveragePartialReason = "structured validation records identify known suites at execution time, but retained history can still contain legacy or unknown observations; coverage is partial rather than inferred complete"
'''),
(
'''const testImpactFoldingReason = "commands with pipelines, environment assignments, or compound chains classify into other under the closed-set rule, so test-category counts are a lower bound and some executed test commands are not distinguishable from other in the retained window"
''',
'''const testImpactFoldingReason = "legacy operation_category can still classify compound shell execution as other; structured validation observations are therefore the primary suite evidence and operation-category test counts remain auxiliary"
'''),
(
'''\t\tTasks:          []TestImpactTaskSummary{},
\t\tCategoryTotals: []TestImpactCategoryMeasure{},
''',
'''\t\tTasks:            []TestImpactTaskSummary{},
\t\tCategoryTotals:   []TestImpactCategoryMeasure{},
\t\tValidationTotals: []TestImpactValidationMeasure{},
'''),
(
'''\ttotals := make(map[string]*TestImpactCategoryMeasure)
''',
'''\ttotals := make(map[string]*TestImpactCategoryMeasure)
\tvalidationTotals := make(map[string]*TestImpactValidationMeasure)
'''),
(
'''\t\treview := reviews[task.TaskID]
''',
'''\t\tvalidations := testImpactValidationMeasures(task)
\t\tfor index := range validations {
\t\t\tabsorbTestImpactValidationMeasure(validationTotals, validations[index])
\t\t\treport.Evaluation.StructuredValidationRuns += validations[index].Runs
\t\t\treport.Evaluation.UnknownValidationRuns += validations[index].Unknown
\t\t\tif validations[index].GateClass == ValidationGateClassUnknown {
\t\t\t\treport.Evaluation.UnknownValidationRuns += validations[index].Runs - validations[index].Unknown
\t\t\t}
\t\t}
\t\treview := reviews[task.TaskID]
'''),
(
'''\t\treport.Tasks = append(report.Tasks, TestImpactTaskSummary{
\t\t\tTaskID:     task.TaskID,
\t\t\tOperations: operations,
\t\t\tReview:     review,
\t\t})
''',
'''\t\treport.Tasks = append(report.Tasks, TestImpactTaskSummary{
\t\t\tTaskID:      task.TaskID,
\t\t\tOperations:  operations,
\t\t\tValidations: validations,
\t\t\tReview:      review,
\t\t})
'''),
(
'''\treport.CategoryTotals = sortedTestImpactMeasures(totals)
\treport.Evaluation.Reasons = testImpactReasons(report)
''',
'''\treport.CategoryTotals = sortedTestImpactMeasures(totals)
\treport.ValidationTotals = sortedTestImpactValidationMeasures(validationTotals)
\tif report.Evaluation.StructuredValidationRuns > 0 {
\t\treport.Evaluation.SuiteCoverage = TestImpactSuiteCoveragePartial
\t}
\treport.Evaluation.Reasons = testImpactReasons(report)
'''),
(
'''func testImpactReasons(report TestImpactReport) []string {
\treasons := []string{testImpactSuiteCoverageReason}
''',
'''func testImpactReasons(report TestImpactReport) []string {
\treasons := []string{}
\tif report.Evaluation.StructuredValidationRuns == 0 {
\t\treasons = append(reasons, testImpactSuiteCoverageReason)
\t} else {
\t\treasons = append(reasons, testImpactSuiteCoveragePartialReason)
\t\treasons = append(reasons, fmt.Sprintf(
\t\t\t"retained structured validation evidence contains %d runs with %d unknown result or gate-class runs",
\t\t\treport.Evaluation.StructuredValidationRuns, report.Evaluation.UnknownValidationRuns))
\t}
'''),
]

for old, new in replacements:
    if text.count(old) != 1:
        raise SystemExit("testimpact anchor mismatch: " + old.splitlines()[0])
    text = text.replace(old, new, 1)

path.write_text(text)
