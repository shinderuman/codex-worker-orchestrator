package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPristineControllerStoreDoesNotMintWorkflowAuthority(t *testing.T) {
	repo := newControllerGuardRepo(t)
	cfg := controllerGuardConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if !controller.IsPristine(before) {
		t.Fatalf("new controller head is not pristine: %#v", before)
	}

	workflow := &Workflow{config: cfg}
	guard, err := workflow.admitControllerModelCall(state.ResumeCheckpoint{})
	if err == nil {
		t.Fatal("mutating model call accepted a pristine controller")
	}
	if guard.active {
		t.Fatal("normal workflow activated a pristine controller store")
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if !controller.IsPristine(after) {
		t.Fatalf("normal workflow minted controller authority: %#v", after)
	}
}

func TestLiveControllerGuardDoesNotFallbackWhenHarnessMarkerIsAbsent(t *testing.T) {
	repo := newControllerGuardRepo(t)
	cfg := controllerGuardConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))
	activated, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}

	workflow := &Workflow{config: cfg}
	guard, err := workflow.admitControllerModelCall(state.ResumeCheckpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if !guard.active {
		t.Fatal("live controller authority fell back to the legacy workflow path")
	}
	if guard.admission.Head.ControllerGeneration <= activated.Head.ControllerGeneration {
		t.Fatalf("model-call admission did not rotate controller generation: before=%d after=%d", activated.Head.ControllerGeneration, guard.admission.Head.ControllerGeneration)
	}
}

func newControllerGuardRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runControllerGuardGit(t, repo, "init", "-q")
	runControllerGuardGit(t, repo, "config", "user.email", "controller-guard@example.invalid")
	runControllerGuardGit(t, repo, "config", "user.name", "Controller Guard Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\ncontroller guard task\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGuardGit(t, repo, "add", ".")
	runControllerGuardGit(t, repo, "commit", "-q", "-m", "base")
	return repo
}

func controllerGuardConfig(repoRoot, stateBase string) config.AppConfig {
	hash := config.RepoHashFor(repoRoot)
	return config.AppConfig{RepoRoot: repoRoot, RepoHash: hash, RepoShort: hash[:12], StateBase: stateBase}
}

func runControllerGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
