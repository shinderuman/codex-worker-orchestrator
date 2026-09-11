package state

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTestImpactReportUsesStructuredValidationEvidence(t *testing.T) {
	base := testImpactBaseTime()
	task := TaskEvents{TaskID: testImpactTaskA, Records: []TaskEventRecord{
		{
			TaskID: testImpactTaskA, CallID: "call-a", Role: "worker", Phase: "worker-new",
			Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []TaskBlockSummary{{
				Type: "tool_use", Name: "Bash", ToolID: "v1", OperationCategory: OperationCategoryOther,
				Validation: []TaskValidationObservation{
					{Form: "go-test", GateClass: ValidationGateClassTest, Suite: "go-test", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial},
					{Form: "go-build", GateClass: ValidationGateClassBuild, Suite: "go-build", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial},
					{Form: "go-vet", GateClass: ValidationGateClassLint, Suite: "go-vet", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial},
					{Form: "tsc", GateClass: ValidationGateClassTypecheck, Suite: "tsc", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial},
				},
			}},
		},
		{
			TaskID: testImpactTaskA, CallID: "call-a", Role: "worker", Phase: "worker-new",
			Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []TaskBlockSummary{{
				Type: "tool_result", Name: "Bash", ToolID: "v1", OperationCategory: OperationCategoryOther, DurationMS: 1200,
				Validation: []TaskValidationObservation{
					{Form: "go-test", GateClass: ValidationGateClassTest, Suite: "go-test", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial, Result: ValidationResultUnknown},
					{Form: "go-build", GateClass: ValidationGateClassBuild, Suite: "go-build", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial, Result: ValidationResultUnknown},
					{Form: "go-vet", GateClass: ValidationGateClassLint, Suite: "go-vet", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial, Result: ValidationResultUnknown},
					{Form: "tsc", GateClass: ValidationGateClassTypecheck, Suite: "tsc", SnapshotID: "snap-1", Phase: "worker-new", Attempt: ValidationAttemptInitial, Result: ValidationResultUnknown},
				},
			}},
		},
		{
			TaskID: testImpactTaskA, CallID: "call-b", Role: "worker", Phase: "worker-explicit-fix",
			Seq: 3, Timestamp: base.Add(2 * time.Second), Kind: "user",
			Blocks: []TaskBlockSummary{{
				Type: "tool_result", Name: "Bash", ToolID: "v2", OperationCategory: OperationCategoryOther, DurationMS: 800,
				Validation: []TaskValidationObservation{{Form: "go-test", GateClass: ValidationGateClassTest, Suite: "go-test", SnapshotID: "snap-1", Phase: "worker-explicit-fix", Attempt: ValidationAttemptRetry, Result: ValidationResultPass}},
			}},
		},
		{
			TaskID: testImpactTaskA, Role: "parent", Phase: "quality-gate", Seq: 4, Timestamp: base.Add(3 * time.Second), Kind: "validation",
			Validation: &TaskValidationEvent{
				Attribution: "task", Source: "quality-gate", Form: "custom-check", ValidationRunID: "run-1",
				GateClass: ValidationGateClassUnknown, Suite: "custom-check", SnapshotID: "snap-2", Phase: "quality-gate",
				Attempt: ValidationAttemptInitial, Result: ValidationResultFail, DurationMS: 300,
			},
		},
	}}

	report := BuildTestImpactReport([]TaskEvents{task}, nil)
	if report.Evaluation.SuiteCoverage != TestImpactSuiteCoveragePartial {
		t.Fatalf("suite coverage = %q", report.Evaluation.SuiteCoverage)
	}
	if report.Evaluation.StructuredValidationRuns != 6 {
		t.Fatalf("structured runs = %d", report.Evaluation.StructuredValidationRuns)
	}
	if report.Evaluation.UnknownValidationRuns != 5 {
		t.Fatalf("unknown runs = %d", report.Evaluation.UnknownValidationRuns)
	}
	if len(report.Tasks) != 1 || len(report.Tasks[0].Validations) != 5 {
		t.Fatalf("validations = %#v", report.Tasks)
	}
	test := findTestImpactValidation(t, report.Tasks[0].Validations, ValidationGateClassTest, "go-test")
	if test.Runs != 2 || test.Initial != 1 || test.Retries != 1 || test.Pass != 1 || test.Unknown != 1 || test.Measured != 1 || test.MeasuredSumMS != 800 {
		t.Fatalf("go-test measure = %#v", test)
	}
	unknown := findTestImpactValidation(t, report.Tasks[0].Validations, ValidationGateClassUnknown, "custom-check")
	if unknown.Runs != 1 || unknown.Fail != 1 || unknown.MeasuredSumMS != 300 {
		t.Fatalf("unknown measure = %#v", unknown)
	}
	for _, measure := range report.ValidationTotals {
		for _, locator := range measure.SourceLocators {
			if strings.Contains(locator, "go test") || strings.Contains(locator, "custom-check --") {
				t.Fatalf("raw command leaked into locator: %q", locator)
			}
			if !strings.HasPrefix(locator, "events/") {
				t.Fatalf("unexpected locator: %q", locator)
			}
		}
	}
	joined := strings.Join(report.Evaluation.Reasons, "\n")
	if !strings.Contains(joined, "structured validation records identify known suites") || !strings.Contains(joined, "coverage is partial") {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func findTestImpactValidation(t *testing.T, values []TestImpactValidationMeasure, gateClass, suite string) TestImpactValidationMeasure {
	t.Helper()
	for _, value := range values {
		if value.GateClass == gateClass && value.Suite == suite {
			return value
		}
	}
	t.Fatalf("validation %s/%s not found: %#v", gateClass, suite, values)
	return TestImpactValidationMeasure{}
}
