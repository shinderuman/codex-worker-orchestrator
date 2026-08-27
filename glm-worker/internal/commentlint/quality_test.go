package commentlint

import (
	"os"
	"path/filepath"
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

func TestCheckKeepsCommentPolicyForOtherRepositories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.go")
	if err := os.WriteFile(path, []byte("package x\n// prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || len(report.Violations) != 1 || report.Violations[0].Kind != "comment" {
		t.Fatalf("report = %+v", report)
	}
}
