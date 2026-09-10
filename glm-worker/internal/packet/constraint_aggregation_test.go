package packet

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWorkerResultAggregatesIndependentConstraintViolations(t *testing.T) {
	result := Result{
		Status:   StatusImplemented,
		Risk:     Risk("MEDIUM"),
		Summary:  "line one\nline two",
		Tests:    "",
		Unverified: "none",
		Targets:  []string{"file.go:10", "file.go:10"},
	}

	err := ValidateWorkerResult(result)
	if err == nil || !IsConstraintError(err) {
		t.Fatalf("constraint errorを期待: %v", err)
	}
	joined := strings.Join(ConstraintReasons(err), "\n")
	for _, want := range []string{
		"riskはLOWまたはHIGH",
		"必須field requirement_coverage",
		"必須field tests",
		"field summaryに改行",
		"TARGETSの要素が重複",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("同時違反 %q が集約されていません: %s", want, joined)
		}
	}
}

func TestValidateArtifactsAggregatesIndependentPathViolations(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	first := filepath.Join(outside, "first.md")
	second := filepath.Join(outside, "second.md")

	err := ValidateArtifacts([]string{first, second}, root)
	if err == nil || !IsConstraintError(err) {
		t.Fatalf("artifact constraint errorを期待: %v", err)
	}
	reasons := ConstraintReasons(err)
	if len(reasons) != 2 || !strings.Contains(reasons[0], first) || !strings.Contains(reasons[1], second) {
		t.Fatalf("artifact違反が個別に集約されていません: %#v", reasons)
	}
}
