package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newReviewerDiffWorkflow(t *testing.T, preexisting string) (*Workflow, string) {
	t.Helper()
	root := t.TempDir()
	gitReviewDiffT(t, root, "init")
	gitReviewDiffT(t, root, "config", "user.email", "test@example.com")
	gitReviewDiffT(t, root, "config", "user.name", "Test")
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("line=base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewDiffT(t, root, "add", "tracked.txt")
	gitReviewDiffT(t, root, "commit", "-m", "base")
	if preexisting != "" {
		if err := os.WriteFile(tracked, []byte(preexisting), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.AppConfig{RepoRoot: root, RepoHash: "review-diff", StateBase: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	return NewWorkflow(cfg, st, nil, nil), root
}

func TestReviewerTaskDiffCapturesCleanBaselineChange(t *testing.T) {
	w, root := newReviewerDiffWorkflow(t, "")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("line=task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := w.captureReviewerTaskDiff()
	if err != nil {
		t.Fatal(err)
	}
	patch := readReviewDiffT(t, path)
	if !strings.Contains(patch, "-line=base") || !strings.Contains(patch, "+line=task") {
		t.Fatalf("task diff missing exact hunk:\n%s", patch)
	}
}

func TestReviewerTaskDiffSubtractsPreexistingWorktreeChange(t *testing.T) {
	w, root := newReviewerDiffWorkflow(t, "line=preexisting\n")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("line=preexisting\nline=task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := w.captureReviewerTaskDiff()
	if err != nil {
		t.Fatal(err)
	}
	patch := readReviewDiffT(t, path)
	if !strings.Contains(patch, "+line=task") {
		t.Fatalf("task addition missing:\n%s", patch)
	}
	if strings.Contains(patch, "+line=preexisting") || strings.Contains(patch, "-line=base") {
		t.Fatalf("pre-task worktree change leaked into task delta:\n%s", patch)
	}
}

func TestReviewerTaskDiffIncludesOnlyTaskCreatedUntrackedFiles(t *testing.T) {
	w, root := newReviewerDiffWorkflow(t, "")
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("before task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(w.config, w.state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("created by task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := w.captureReviewerTaskDiff()
	if err != nil {
		t.Fatal(err)
	}
	patch := readReviewDiffT(t, path)
	if !strings.Contains(patch, "new.txt") || !strings.Contains(patch, "+created by task") {
		t.Fatalf("task-created untracked file missing:\n%s", patch)
	}
	if strings.Contains(patch, "existing.txt") {
		t.Fatalf("pre-existing untracked file leaked into task delta:\n%s", patch)
	}
}

func gitReviewDiffT(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func readReviewDiffT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
