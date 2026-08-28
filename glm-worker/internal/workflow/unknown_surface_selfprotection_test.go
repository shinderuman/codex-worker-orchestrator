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
		"allowlist.txt",
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
		"notes.md",
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

func TestUnknownSurfaceRaisesRiskFloorBeforeCompletion(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
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
	if got := strings.Join(r.phases, ","); got != "worker-new,reviewer-1-high-floor" {
		t.Fatalf("phases = %q", got)
	}
}

func TestUnknownSurfaceHighFloorAllowsFixRequiredBeforeSolEscalation(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"runtime/new-worker.py"}, nil
	}
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	want := "worker-new,reviewer-1-high-floor,worker-auto-fix-1,reviewer-2-high-floor"
	if got := strings.Join(r.phases, ","); got != want {
		t.Fatalf("phases = %q, want %q", got, want)
	}
	if !strings.Contains(r.prompts[1], "WRAPPER_EFFECTIVE_RISK_FLOOR: HIGH") {
		t.Fatalf("high-floor prompt missing: %s", r.prompts[1])
	}
}

func TestKnownSafeLowRiskReviewKeepsNormalPassPath(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"docs/guide.md"}, nil
	}
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.phases, ","); got != "worker-new,reviewer-1" {
		t.Fatalf("phases = %q", got)
	}
	if strings.Contains(r.prompts[1], "WRAPPER_EFFECTIVE_RISK_FLOOR") {
		t.Fatalf("normal reviewer received high-floor prompt: %s", r.prompts[1])
	}
}
