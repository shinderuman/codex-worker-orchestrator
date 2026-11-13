package harnesslint

import (
	"fmt"
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
	plan, tasks, target := canonicalRetirementFixture(t, workspace)
	retirement, err := repositoryproject.RetireTerminalMetadata(plan, tasks, target, true)
	if err != nil {
		t.Fatal(err)
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

func canonicalRetirementFixture(t *testing.T, workspace string) ([]byte, map[string][]byte, string) {
	t.Helper()
	planPath := filepath.Join(workspace, "IMPLEMENTATION_PLAN.local.md")
	original, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(original))
	activeEntries, err := schedule.ActiveEntries()
	if err != nil {
		t.Fatal(err)
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := os.ReadDir(filepath.Join(workspace, "IMPLEMENTATION_TASKS"))
	if err != nil {
		t.Fatal(err)
	}
	tasks := make(map[string][]byte)
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
	active := "IMPLEMENTATION_TASKS/retirement-lint-active.md"
	switch len(activeEntries) {
	case 0:
		if _, exists := tasks[active]; exists {
			t.Fatalf("fixture ACTIVE already exists: %s", active)
		}
		tasks[active] = []byte("# 退役検証fixture\n\n## Original instruction\n\n退役候補の正規lintを検証する。\n\n## Amendments\n\n## Purpose\n\n退役検証。\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\n- 正規lintを通す。\n\n## Must not\n\n- 本番を変更しない。\n\n## Acceptance criteria\n\n- 正規lint成功。\n\n## Historical invariants\n\n- fixture内に限定する。\n\n## Dependencies\n\nnone\n")
	case 1:
		active = activeEntries[0]
	default:
		t.Fatal("fixture source has multiple ACTIVE tasks")
	}
	target := "IMPLEMENTATION_TASKS/retirement-lint-fixture.md"
	if _, exists := tasks[target]; exists {
		t.Fatalf("fixture target already exists: %s", target)
	}
	content, exists := tasks[active]
	if !exists {
		t.Fatalf("fixture ACTIVE task is missing: %s", active)
	}
	tasks[target] = append([]byte(nil), content...)
	plan := retirementFixtureSchedule(active, append(next, target), blocked)
	for path, data := range map[string][]byte{active: tasks[active], target: tasks[target], "IMPLEMENTATION_PLAN.local.md": plan} {
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	retirementGit(t, workspace, "add", "--all")
	retirementGit(t, workspace, "-c", "user.name=retirement-lint", "-c", "user.email=retirement-lint@example.invalid", "commit", "-qm", "prepare retirement fixture")
	return plan, tasks, target
}

func retirementFixtureSchedule(active string, next, blocked []string) []byte {
	var plan strings.Builder
	plan.WriteString("# retirement lint fixture\n\n")
	for _, section := range []struct {
		heading string
		paths   []string
	}{
		{"ACTIVE", []string{active}},
		{"NEXT（優先順）", next},
		{"BLOCKED / USER_PERMISSION_WAIT", blocked},
	} {
		fmt.Fprintf(&plan, "## %s\n\n", section.heading)
		for _, path := range section.paths {
			fmt.Fprintf(&plan, "- [`%s`](%s)\n", path, path)
		}
		plan.WriteByte('\n')
	}
	return []byte(plan.String())
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
