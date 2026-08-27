package commentlint

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

func TestQualityReportPreservesHarnesslintViolation(t *testing.T) {
	source := harnesslint.Report{
		Status: "fail",
		Fixed:  2,
		Violations: []harnesslint.Violation{{
			Rule: "prose-contract-pin", Path: "a_test.go", Line: 7, Column: 3, Message: "pin",
		}},
	}
	got := qualityReport(source)
	if got.Status != "fail" || got.Fixed != 2 || len(got.Violations) != 1 {
		t.Fatalf("report = %+v", got)
	}
	violation := got.Violations[0]
	if violation.Kind != "prose-contract-pin" || violation.Path != "a_test.go" || violation.Line != 7 || violation.Column != 3 || violation.Message != "pin" {
		t.Fatalf("violation = %+v", violation)
	}
}
