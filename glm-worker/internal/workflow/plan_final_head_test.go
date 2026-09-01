package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalHeadPlanVerified(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan("main", ""), true)
	commitFinalHeadFixture(t, root)

	status, err := CheckFinalHeadPlan(root)
	if err != nil || status != "plan final head: verified" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestFinalHeadPlanRejectsMissingTask(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan("main", ""), false)
	commitFinalHeadFixture(t, root)

	_, err := CheckFinalHeadPlan(root)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalHeadPlanRejectsBranchMismatch(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan("other", ""), true)
	commitFinalHeadFixture(t, root)

	_, err := CheckFinalHeadPlan(root)
	if err == nil || !strings.Contains(err.Error(), "branch other") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalHeadPlanRejectsDuplicateActiveSchedule(t *testing.T) {
	root := newFinalHeadRepo(t)
	plan := finalHeadFixturePlan("main", "- `IMPLEMENTATION_TASKS/active.md`\n")
	writeFinalHeadFixture(t, root, plan, true)
	commitFinalHeadFixture(t, root)

	_, err := CheckFinalHeadPlan(root)
	if err == nil || !strings.Contains(err.Error(), "重複") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalHeadPlanRejectsMalformedNonActiveSchedule(t *testing.T) {
	root := newFinalHeadRepo(t)
	plan := strings.Replace(finalHeadFixturePlan("main", ""), "- `IMPLEMENTATION_TASKS/next.md`", "not a schedule bullet", 1)
	writeFinalHeadFixture(t, root, plan, true)
	commitFinalHeadFixture(t, root)

	_, err := CheckFinalHeadPlan(root)
	if err == nil || !strings.Contains(err.Error(), "NEXT欄") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalHeadPlanRejectsTransitionalState(t *testing.T) {
	root := newFinalHeadRepo(t)
	plan := finalHeadFixturePlan("main", "") + "\n## 次の親Codex操作\n\n- install前に停止する\n"
	writeFinalHeadFixture(t, root, plan, true)
	commitFinalHeadFixture(t, root)

	_, err := CheckFinalHeadPlan(root)
	if err == nil || !strings.Contains(err.Error(), "未実施") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalHeadPlanSkipsNonGitRepository(t *testing.T) {
	status, err := CheckFinalHeadPlan(t.TempDir())
	if err != nil || status != "plan final head: skipped (not a git repository)" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func finalHeadFixturePlan(branch string, extraNext string) string {
	return "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/next.md`\n" + extraNext + "\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在のGit境界\n\n- branch: `" + branch + "`\n\n" +
		"## 現在の停止理由\n\nなし\n"
}

func newFinalHeadRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runFinalHeadGit(t, root, "init", "-q", "-b", "main")
	runFinalHeadGit(t, root, "config", "user.name", "test")
	runFinalHeadGit(t, root, "config", "user.email", "test@example.com")
	return root
}

func writeFinalHeadFixture(t *testing.T, root string, plan string, includeNext bool) {
	t.Helper()
	writeFinalHeadFile(t, root, implementationPlanFile, plan)
	writeFinalHeadFile(t, root, "IMPLEMENTATION_TASKS/active.md", "# active\n")
	if includeNext {
		writeFinalHeadFile(t, root, "IMPLEMENTATION_TASKS/next.md", "# next\n")
	}
}

func writeFinalHeadFile(t *testing.T, root string, path string, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitFinalHeadFixture(t *testing.T, root string) {
	t.Helper()
	runFinalHeadGit(t, root, "add", "-A")
	runFinalHeadGit(t, root, "commit", "-q", "-m", "fixture")
}

func runFinalHeadGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
