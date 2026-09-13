package repositoryprojecthead

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPreparedValidationUsesCapturedHead(t *testing.T) {
	root := t.TempDir()
	headTestGit(t, root, "init", "-q", "-b", "main")
	headTestGit(t, root, "config", "user.name", "test")
	headTestGit(t, root, "config", "user.email", "test@example.com")
	writeHeadTestFile(t, root, state.ParentPlanFile, "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n## NEXT\n\n## BLOCKED\n")
	writeHeadTestFile(t, root, "IMPLEMENTATION_TASKS/active.md", "# active\n\n## External feasibility\n\nstatus: not-applicable\n")
	headTestGit(t, root, "add", "-A")
	headTestGit(t, root, "commit", "-q", "-m", "valid")

	snapshot, err := finalHeadPlan(root)
	if err != nil || !snapshot.Present {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	prepared, err := repositoryproject.PrepareFinalHead(snapshot.Plan)
	if err != nil {
		t.Fatal(err)
	}

	writeHeadTestFile(t, root, "IMPLEMENTATION_TASKS/active.md", "# active\n")
	headTestGit(t, root, "add", "-A")
	headTestGit(t, root, "commit", "-q", "-m", "invalid")
	if _, err := CheckFinalHeadPlan(root); err == nil {
		t.Fatal("current HEAD unexpectedly validated")
	}
	if err := validatePreparedPlan(root, snapshot.Head, prepared); err != nil {
		t.Fatalf("captured HEAD validation followed mutable HEAD: %v", err)
	}
}

func writeHeadTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func headTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git command failed: %v: %s", err, output)
	}
}
