package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestClassifySelfProtectionFailsClosedUnknownSurfaces(t *testing.T) {
	for _, path := range []string{
		".github/workflows/new-quality.yml",
		"runtime/new-worker.py",
		"tooling/deploy.sh",
		"config/runtime.yaml",
		"new-area/custom.extension",
		"requirements.txt",
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
		"uncommitted.txt",
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

func TestUnknownSurfaceRaisesRiskFloorBeforeCompletion(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"runtime/new-worker.py"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if got := strings.Join(r.phases, ","); got != "worker-new,reviewer-1,reviewer-1-risk-floor" {
		t.Fatalf("phases = %q", got)
	}
}
