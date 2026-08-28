package workflow

import (
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAcceptedFixScopeSkipsRedundantRiskFloorCall(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "scope-test")
	writeScopeFile(t, repo, "code.go", "package sample\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "scope-risk"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar retained = 1\nvar removeMe = 2\n")

	runner := &scriptedRunner{}
	w := NewWorkflow(cfg, st, runner, io.Discard)
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar retained = 1\n")

	result, stopped, err := w.enforceRiskFloor("request", packet.Result{}, 1, 0, "none", true, packet.Result{Status: packet.StatusPass})
	if err != nil || stopped || result.Status != packet.StatusPass {
		t.Fatalf("result = %#v, stopped=%v, err=%v", result, stopped, err)
	}
	if len(runner.phases) != 0 {
		t.Fatalf("redundant risk-floor model call phases = %v", runner.phases)
	}
}
