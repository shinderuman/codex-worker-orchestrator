package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestQualityWiringRequiresReviewerGate(t *testing.T) {
	root := t.TempDir()
	path := "glm-worker/internal/workflow/workflow.go"
	writeQualityFile(t, root, path, "package workflow\n")
	violations, err := qualityWiringViolations(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 13 {
		t.Fatalf("violations = %+v", violations)
	}
}

func writeQualityFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := execfile.Write(absolute, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestQualityWiringPackageAllowsResponsibilitySplit(t *testing.T) {
	root := t.TempDir()
	workflowPath := "glm-worker/internal/workflow/workflow.go"
	reviewPath := "glm-worker/internal/workflow/review_flow.go"
	writeQualityFile(t, root, workflowPath, "package workflow\nfunc (w *Workflow) start() { w.captureQualitySurfaceBaseline(); w.verifyQualitySurfaceBaseline(workerPhase) }\n")
	writeQualityFile(t, root, reviewPath, "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc (handler *Workflow) review() { handler.qualityGate(handler.config.RepoRoot); harnesslint.IsViolation(qualityReport) }\n")
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

func TestQualityWiringAcceptsSingleFixAndValidationPass(t *testing.T) {
	root := t.TempDir()
	check := qualityWiringChecks()[0]
	writeQualityFile(t, root, check.path, "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(root, true); captureQualitySurfaceDigest(root) }\n")

	violations, err := qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("single validation pass was rejected: %+v", violations)
	}
}

func TestQualityWiringRejectsDuplicateValidationPass(t *testing.T) {
	root := t.TempDir()
	check := qualityWiringChecks()[0]
	writeQualityFile(t, root, check.path, "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(root, true); harnesslint.Check(root); captureQualitySurfaceDigest(root) }\n")

	violations, err := qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		if strings.Contains(violation.Message, "forbidden quality-gate wiring is present") {
			return
		}
	}
	t.Fatalf("duplicate validation pass was not rejected: %+v", violations)
}

func TestQualityWiringRejectsUnboundHarnesslintCall(t *testing.T) {
	root := t.TempDir()
	check := qualityWiringChecks()[0]
	writeQualityFile(t, root, check.path, "package workflow\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(root, true); captureQualitySurfaceDigest(root) }\n")
	violations, err := qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if strings.Contains(v.Message, "required quality-gate wiring is missing: harnesslint.Run") {
			return
		}
	}
	t.Fatalf("unbound package impersonation bypassed the quality gate: %+v", violations)
}

func TestQualityWiringRecognizesCanonicalImportAlias(t *testing.T) {
	root := t.TempDir()
	check := qualityWiringChecks()[0]
	const source = "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc runRepositoryQualityGate(root string) { gate.Run(root, true); captureQualitySurfaceDigest(root) }\n"
	writeQualityFile(t, root, check.path, source)
	violations, err := qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
	if err != nil || len(violations) != 0 {
		t.Fatalf("equivalent harnesslint import alias was rejected: %+v err=%v", violations, err)
	}
	writeQualityFile(t, root, check.path, strings.Replace(source, "gate.Run(root, true);", "gate.Run(root, true); gate.Check(root);", 1))
	violations, err = qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if strings.Contains(v.Message, "forbidden quality-gate wiring is present") {
			return
		}
	}
	t.Fatalf("forbidden check escaped detection through an import alias: %+v", violations)
}

func TestQualityWiringPackageDoesNotAcceptUnboundOrWrongImport(t *testing.T) {
	const workflowPath = "glm-worker/internal/workflow/workflow.go"
	const reviewPath = "glm-worker/internal/workflow/review_flow.go"
	for _, tc := range []struct {
		name, importLine, call string
		valid                  bool
	}{
		{"unbound", "", "harnesslint.IsViolation(qualityReport)", false},
		{"wrong-import", "import \"example.invalid/harnesslint\"", "harnesslint.IsViolation(qualityReport)", false},
		{"canonical-alias", "import quality \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"", "quality.IsViolation(qualityReport)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeQualityFile(t, root, workflowPath, "package workflow\nfunc (w *Workflow) start() { w.captureQualitySurfaceBaseline(); w.verifyQualitySurfaceBaseline(workerPhase) }\n")
			writeQualityFile(t, root, reviewPath, "package workflow\n"+tc.importLine+"\nfunc (w *Workflow) review() { w.qualityGate(w.config.RepoRoot); "+tc.call+" }\n")
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
			if tc.valid && len(violations) != 0 {
				t.Fatalf("canonical quality wiring alias rejected: %+v", violations)
			}
			if !tc.valid {
				for _, violation := range violations {
					if strings.Contains(violation.Message, "harnesslint.IsViolation") {
						return
					}
				}
				t.Fatalf("unbound package satisfied quality wiring: %+v", violations)
			}
		})
	}
}
