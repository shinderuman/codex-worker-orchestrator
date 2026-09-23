package parentcontinuation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestContinuationCompletionViewPreservesProjectCompletionEvidence(t *testing.T) {
	repoRoot := t.TempDir()
	writeContinuationRepoFile(t, repoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeContinuationRepoFile(t, repoRoot, "IMPLEMENTATION_TASKS/final.md", "# Task\n\n## Dependencies\n\nnone\n\n## Fulfilled dependencies\n\nnone\n")
	commitContinuationRepo(t, repoRoot)

	cfg := config.AppConfig{RepoRoot: repoRoot, RepoHash: config.RepoHashFor(repoRoot), StateBase: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repositoryprojecttree.LoadProjectState(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := continuationCompletionView(repoRoot, st, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if completion == nil || completion.Ready {
		t.Fatalf("completion = %#v", completion)
	}
	unmet := strings.Join(completion.Unmet, ",")
	for _, want := range []string{"task_not_complete", "active_task_mismatch", "validation_not_current"} {
		if !strings.Contains(unmet, want) {
			t.Fatalf("unmet = %q", unmet)
		}
	}
}

func writeContinuationRepoFile(t *testing.T, repoRoot, path, content string) {
	t.Helper()
	target := filepath.Join(repoRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitContinuationRepo(t *testing.T, repoRoot string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "fixture"},
	} {
		command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}
