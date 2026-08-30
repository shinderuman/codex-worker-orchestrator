package workflow

import (
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualitySurfaceChangeAllowsParentAcceptedCurrentDiff(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "quality-scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "quality-scope-test")
	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 0\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "quality-scope"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "baseline"); err != nil {
		t.Fatal(err)
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexport GOCACHE=/tmp/go-cache\nexit 0\n")
	w := NewWorkflow(cfg, st, nil, io.Discard)
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	if !st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("parent accepted current diff was not captured")
	}
	w.captureQualitySurface = func(string) (string, error) { return "approved", nil }

	stopped, err := w.verifyQualitySurfaceBaseline("worker-explicit-fix")
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("parent-approved quality surface must proceed to independent review")
	}
	if got := st.ReadOr(qualitySurfaceBaselineStateKey, ""); got != "approved" {
		t.Fatalf("quality surface baseline = %q", got)
	}
	if !st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("quality surface approval must not consume accepted scope before risk evaluation")
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexport GOCACHE=/tmp/other-cache\nexit 0\n")
	w.captureQualitySurface = func(string) (string, error) { return "expanded", nil }
	stopped, err = w.verifyQualitySurfaceBaseline("worker-auto-fix-1")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("quality surface changes outside the parent-approved diff must fail closed")
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}
