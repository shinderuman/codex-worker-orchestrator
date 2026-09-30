package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestControllerRejectsDerivedNewTaskBeforeLegacyStateAdmission(t *testing.T) {
	primary := initGitRepo(t)
	writeControllerAdmissionFile(t, primary, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeControllerAdmissionFile(t, primary, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/controller-test.md`\n")
	writeControllerAdmissionFile(t, primary, "IMPLEMENTATION_TASKS/controller-test.md", "# controller test\n\n## Contract\n\ncontroller test\n")
	commitControllerAdmissionRepo(t, primary)

	derived := filepath.Join(t.TempDir(), "derived")
	if output, err := exec.Command("git", "-C", primary, "worktree", "add", "--quiet", "--detach", derived).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, output)
	}

	cfg := config.AppConfig{
		StateBase:             t.TempDir(),
		RepoHash:              config.RepoHashFor(derived),
		RepoRoot:              derived,
		RepoShort:             config.RepoHashFor(derived)[:12],
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		CodexConfigDir:        t.TempDir(),
	}
	runner := &fakeRunner{}
	err := Execute(Command{Mode: ModeNewTask, Payload: "must not start"}, cfg, runner.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "verified primary worktree") {
		t.Fatalf("derived new-task rejection = %v", err)
	}

	st := state.AttachStateStore(cfg)
	for _, name := range []string{"task.id", "task.status", "active-task"} {
		if st.Exists(name) {
			t.Fatalf("derived new-task mutated legacy task state before controller rejection: %s", name)
		}
	}
	if len(runner.prompts) != 0 {
		t.Fatalf("derived new-task reached model dispatch: prompts=%d", len(runner.prompts))
	}
}

func writeControllerAdmissionFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitControllerAdmissionRepo(t *testing.T, root string) {
	t.Helper()
	if output, err := exec.Command("git", "-C", root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command(
		"git", "-C", root,
		"-c", "user.name=controller admission test",
		"-c", "user.email=controller-admission@example.invalid",
		"commit", "--quiet", "-m", "controller admission fixture",
	).CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
}
