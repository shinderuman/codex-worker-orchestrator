package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestGeneratedTerminalRetirementCommitPassesCanonicalLint(t *testing.T) {
	if os.Getenv("QUALITY_TOOLS_BIN_DIR") == "" {
		t.Skip("canonical quality tool installation is not available")
	}
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(current, "..", "..", ".."))
	sourceOID := retirementGit(t, root, "rev-parse", "HEAD")
	workspace, err := newCommitWorkspace(root, sourceOID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	plan, err := os.ReadFile(filepath.Join(workspace, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := os.ReadDir(filepath.Join(workspace, "IMPLEMENTATION_TASKS"))
	if err != nil {
		t.Fatal(err)
	}
	tasks := map[string][]byte{}
	for _, entry := range paths {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := "IMPLEMENTATION_TASKS/" + entry.Name()
		content, err := os.ReadFile(filepath.Join(workspace, path))
		if err != nil {
			t.Fatal(err)
		}
		tasks[path] = content
	}
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	if len(schedule.Next) < 1 {
		t.Fatal("canonical corpus lacks a NEXT task for retirement")
	}
	var retirement repositoryproject.TerminalMetadata
	var target string
	for _, path := range schedule.Next {
		retired, err := repositoryproject.RetireTerminalMetadata(plan, tasks, path, true)
		if err == nil {
			target, retirement = path, retired
			break
		}
	}
	if target == "" {
		t.Fatal("no existing NEXT task admits a mechanical terminal retirement")
	}
	for _, change := range retirement.Changes {
		path := filepath.Join(workspace, change.Path)
		if change.Delete {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, change.NewBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	retirementGit(t, workspace, "add", "--all")
	retirementGit(t, workspace, "-c", "user.name=retirement-lint", "-c", "user.email=retirement-lint@example.invalid", "commit", "-qm", "retire fixture task")
	finalOID := retirementGit(t, workspace, "rev-parse", "HEAD")
	report, err := RunCommit(workspace, finalOID)
	if err != nil || report.Status != "pass" || len(report.Violations) != 0 {
		t.Fatalf("generated terminal retirement commit failed canonical lint: %s, %#v, %v", target, report.Violations, err)
	}
}

func retirementGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
