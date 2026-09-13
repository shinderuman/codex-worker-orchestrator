package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalHeadPlanSkipsGenuineUnbornRepository(t *testing.T) {
	root := newFinalHeadRepo(t)

	requireFinalHeadSkip(t, root, "skipped (no commits)")
}

func TestFinalHeadPlanSkipsGenuinelyUntrackedPlan(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFile(t, root, "README.md", "base\n")
	commitFinalHeadFixture(t, root)
	writeFinalHeadFile(t, root, implementationPlanFile, finalHeadFixturePlan(""))

	requireFinalHeadSkip(t, root, "skipped (IMPLEMENTATION_PLAN.local.md is untracked)")
}

func TestFinalHeadPlanSkipsTrackedPlanNotYetInHead(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFile(t, root, "README.md", "base\n")
	commitFinalHeadFixture(t, root)
	writeFinalHeadFile(t, root, implementationPlanFile, finalHeadFixturePlan(""))
	runFinalHeadGit(t, root, "add", implementationPlanFile)

	requireFinalHeadSkip(t, root, "skipped (IMPLEMENTATION_PLAN.local.md is not in HEAD yet)")
}

func TestFinalHeadPlanRejectsBrokenHeadAuthority(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan(""), true)
	commitFinalHeadFixture(t, root)

	refsHeads := filepath.Join(root, ".git", "refs", "heads")
	if err := os.WriteFile(filepath.Join(refsHeads, "broken"), []byte("feedfacefeedfacefeedfacefeedfacefeedface\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	requireFinalHeadError(t, root, "final HEAD authorityを確認できません")
}

func TestFinalHeadPlanRejectsUnexpectedIndexFailure(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan(""), true)
	commitFinalHeadFixture(t, root)

	index := filepath.Join(root, ".git", "index")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}

	requireFinalHeadError(t, root, "index追跡状態を確認できません")
}

func TestFinalHeadPlanRejectsHeadPlanObjectReadFailure(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFixture(t, root, finalHeadFixturePlan(""), true)
	commitFinalHeadFixture(t, root)

	blob := finalHeadGitTestOutput(t, root, "rev-parse", "HEAD:"+implementationPlanFile)
	if len(blob) < 3 {
		t.Fatalf("plan blob id = %q", blob)
	}
	objectPath := filepath.Join(root, ".git", "objects", blob[:2], blob[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}

	requireFinalHeadError(t, root, "HEADのIMPLEMENTATION_PLAN.local.mdを読めません")
}

func requireFinalHeadSkip(t *testing.T, root string, suffix string) {
	t.Helper()
	checks := []struct {
		name   string
		prefix string
		check  func(string) (string, error)
	}{
		{name: "final", prefix: "plan final head: ", check: CheckFinalHeadPlan},
		{name: "completion", prefix: "plan completion head: ", check: CheckParentCompletionHead},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			status, err := tc.check(root)
			if err != nil || status != tc.prefix+suffix {
				t.Fatalf("status=%q err=%v", status, err)
			}
		})
	}
}

func requireFinalHeadError(t *testing.T, root string, want string) {
	t.Helper()
	checks := []struct {
		name  string
		check func(string) (string, error)
	}{
		{name: "final", check: CheckFinalHeadPlan},
		{name: "completion", check: CheckParentCompletionHead},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			status, err := tc.check(root)
			if err == nil || status != "" || !strings.Contains(err.Error(), want) {
				t.Fatalf("status=%q err=%v want error containing %q", status, err, want)
			}
		})
	}
}

func finalHeadGitTestOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
