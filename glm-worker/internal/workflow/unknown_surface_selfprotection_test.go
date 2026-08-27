package workflow

import "testing"

func TestClassifySelfProtectionFailsClosedUnknownSurfaces(t *testing.T) {
	for _, path := range []string{
		".github/workflows/new-quality.yml",
		"runtime/new-worker.py",
		"tooling/deploy.sh",
		"config/runtime.yaml",
		"new-area/custom.extension",
	} {
		t.Run(path, func(t *testing.T) {
			decision := classifySelfProtection([]string{path})
			if !decision.High || decision.Source != "unknown-surface" || decision.HitPath != path {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestClassifySelfProtectionKeepsKnownSafeLow(t *testing.T) {
	for _, path := range []string{
		"docs/guide.md",
		"pkg/worker_test.py",
		"frontend/widget.spec.ts",
		"src/tests/helper.js",
		"fixtures/sample.json",
	} {
		t.Run(path, func(t *testing.T) {
			decision := classifySelfProtection([]string{path})
			if decision.High {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestClassifySelfProtectionAggregatesUnknownWithKnownCritical(t *testing.T) {
	decision := classifySelfProtection([]string{
		"runtime/new-worker.py",
		"glm-worker/internal/workflow/workflow.go",
	})
	if !decision.High || decision.Source != "unknown-surface,workflow-package" {
		t.Fatalf("decision = %#v", decision)
	}
}
