package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualityWiringRequiresReviewerGate(t *testing.T) {
	root := t.TempDir()
	path := "glm-worker/internal/workflow/workflow.go"
	writeQualityFile(t, root, path, "package workflow\n")
	violations, err := qualityWiringViolations(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 12 {
		t.Fatalf("violations = %+v", violations)
	}
}

func writeQualityFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestQualityWiringPackageAllowsResponsibilitySplit(t *testing.T) {
	root := t.TempDir()
	workflowPath := "glm-worker/internal/workflow/workflow.go"
	reviewPath := "glm-worker/internal/workflow/review_flow.go"
	writeQualityFile(t, root, workflowPath, "package workflow\nfunc start() { w.captureQualitySurfaceBaseline(); w.verifyQualitySurfaceBaseline(workerPhase) }\n")
	writeQualityFile(t, root, reviewPath, "package workflow\nfunc review() { w.qualityGate(w.config.RepoRoot); harnesslint.IsViolation(qualityReport) }\n")
	present := map[string]bool{workflowPath: true, reviewPath: true}
	violations, err := qualityWiringPackageViolations(root, present, "glm-worker/internal/workflow/", []string{
		"w.captureQualitySurfaceBaseline()",
		"w.verifyQualitySurfaceBaseline(workerPhase)",
		"w.qualityGate(w.config.RepoRoot)",
		"harnesslint.IsViolation(qualityReport)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %+v", violations)
	}
}
