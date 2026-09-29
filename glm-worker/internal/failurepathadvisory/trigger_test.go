package failurepathadvisory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTriggerRepo(t *testing.T, baselineFiles map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	runTriggerGit(t, root, "init")
	runTriggerGit(t, root, "config", "user.email", "trial@example.invalid")
	runTriggerGit(t, root, "config", "user.name", "trial")
	for path, content := range baselineFiles {
		writeTriggerFile(t, root, path, content)
	}
	runTriggerGit(t, root, "add", ".")
	runTriggerGit(t, root, "commit", "-m", "baseline")
	return root, strings.TrimSpace(runTriggerGit(t, root, "rev-parse", "HEAD"))
}

func runTriggerGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, output)
	}
	return string(output)
}

func writeTriggerFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyTriggerRequiresPathAndDiffContent(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar callSite = newProcessGroupCmd\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/runner/call.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Triggered || len(decision.Classes) != 1 || decision.Classes[0] != ClassExternalModelInvocation {
		t.Fatalf("decision = %+v", decision)
	}
	if len(decision.TriggerPaths) != 1 {
		t.Fatalf("trigger paths = %+v", decision.TriggerPaths)
	}
}

func TestClassifyTriggerPathOnlyRunnerMatchIsAmbiguous(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n\nvar alpha = 1\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar beta = 1\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/runner/call.go"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Triggered {
		t.Fatalf("path一致だけてtriggeredになりました: %+v", decision)
	}
	if len(decision.AmbiguousClasses) != 1 || decision.AmbiguousClasses[0] != ClassExternalModelInvocation {
		t.Fatalf("ambiguous classes = %+v", decision.AmbiguousClasses)
	}
}

func TestClassifyTriggerWorkflowPathAloneDoesNotTrigger(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/workflow/plan.go": "package workflow\n\nconst label = \"a\"\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/workflow/plan.go", "package workflow\n\nconst label = \"b\"\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/workflow/plan.go"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Triggered {
		t.Fatalf("workflow path一致だけてtriggeredになりました: %+v", decision)
	}
	if len(decision.AmbiguousClasses) != 1 || decision.AmbiguousClasses[0] != ClassReviewLifecycleRouting {
		t.Fatalf("ambiguous classes = %+v", decision.AmbiguousClasses)
	}
}

func TestClassifyTriggerWorkflowLifecycleContentTriggers(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/workflow/review_extra.go": "package workflow\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/workflow/review_extra.go", "package workflow\n\nfunc extra() { _ = state.ResumeCheckpoint{} }\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/workflow/review_extra.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Triggered || decision.Classes[0] != ClassReviewLifecycleRouting {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestClassifyTriggerIgnoresTestPaths(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call_test.go": "package runner\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call_test.go", "package runner\n\nconst _ = \"newProcessGroupCmd\"\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/runner/call_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Triggered || len(decision.AmbiguousClasses) != 0 {
		t.Fatalf("test pathが候補になりました: %+v", decision)
	}
}

func TestClassifyTriggerUntrackedFileContentConfirms(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"README.md": "readme\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/abeval/usage_extra.go", "package abeval\n\nvar usage = Usage{Tokens: 1}\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"glm-worker/internal/abeval/usage_extra.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Triggered || decision.Classes[0] != ClassMetricReductionAccounting {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestClassifyTriggerNoClassPathLeavesNoRecord(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"README.md": "readme\n",
	})
	writeTriggerFile(t, root, "README.md", "readme updated\n")

	decision, err := ClassifyTrigger(root, baseline, []string{"README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Triggered || len(decision.AmbiguousClasses) != 0 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestClassifyTriggerDiffFailureIsUndecidable(t *testing.T) {
	root, _ := newTriggerRepo(t, map[string]string{"README.md": "readme\n"})
	_, err := ClassifyTrigger(root, "definitely-not-a-commit", []string{"glm-worker/internal/runner/call.go"})
	if err == nil {
		t.Fatal("不正baselineでerrorが返りませんでした")
	}
}

func TestDiffTextForPathsReturnsBaselineDiff(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar callSite = newProcessGroupCmd\n")

	text, err := DiffTextForPaths(root, baseline, []string{"glm-worker/internal/runner/call.go"}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "newProcessGroupCmd") || !strings.Contains(text, "glm-worker/internal/runner/call.go") {
		t.Fatalf("diff text = %q", text)
	}
}

func TestDiffTextForPathsUntrackedOverLimitFails(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{"README.md": "readme\n"})
	writeTriggerFile(t, root, "glm-worker/internal/runner/big.go", "package runner\n\nvar big = \""+strings.Repeat("x", 5000)+"\"\n")

	if _, err := DiffTextForPaths(root, baseline, []string{"glm-worker/internal/runner/big.go"}, 1024); err == nil || !strings.Contains(err.Error(), "読取上限") {
		t.Fatalf("untracked読取上限超過 = %v", err)
	}
}

func TestDiffTextForPathsTrackedDiffOverLimitFails(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar big = \""+strings.Repeat("x", 5000)+"\"\n")

	if _, err := DiffTextForPaths(root, baseline, []string{"glm-worker/internal/runner/call.go"}, 1024); err == nil || !strings.Contains(err.Error(), "読取上限") {
		t.Fatalf("tracked diff読取上限超過 = %v", err)
	}
}

func TestDiffTextForPathsWithinLimitReturnsWholeDiff(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{"README.md": "readme\n"})
	content := "package runner\n\nvar big = \"" + strings.Repeat("x", 900) + "\"\n"
	writeTriggerFile(t, root, "glm-worker/internal/runner/big.go", content)

	text, err := DiffTextForPaths(root, baseline, []string{"glm-worker/internal/runner/big.go"}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, content) {
		t.Fatalf("上限内のdiff全文が取得できていません: size = %d", len(text))
	}
}

func TestDiffTextForPathsCommandDeadlineFails(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
	})
	previous := diffCommandTimeout
	diffCommandTimeout = time.Nanosecond
	defer func() { diffCommandTimeout = previous }()

	if _, err := DiffTextForPaths(root, baseline, []string{"glm-worker/internal/runner/call.go"}, 4096); err == nil {
		t.Fatal("git deadline超過でerrorが返りませんでした")
	}
}

func TestDiffTextForPathsInvalidBaselineFails(t *testing.T) {
	root, _ := newTriggerRepo(t, map[string]string{"README.md": "readme\n"})
	if _, err := DiffTextForPaths(root, "definitely-not-a-commit", []string{"glm-worker/internal/runner/call.go"}, 1024); err == nil {
		t.Fatal("不正baseline(snapshot不一致)でerrorが返りませんでした")
	}
}

func TestDiffTextForPathsTrackedAtCapWithUntrackedChangeFails(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar big = \""+strings.Repeat("x", 2000)+"\"\n")
	writeTriggerFile(t, root, "glm-worker/internal/abeval/usage_extra.go", "package abeval\n\nvar usage = Usage{Tokens: 1}\n")
	paths := []string{"glm-worker/internal/runner/call.go", "glm-worker/internal/abeval/usage_extra.go"}
	diffOut := runTriggerGit(t, root, append([]string{"diff", "--no-renames", baseline, "--"}, paths...)...)

	if _, err := DiffTextForPaths(root, baseline, paths, len(diffOut)); err == nil || !strings.Contains(err.Error(), "読取上限") {
		t.Fatalf("budget使い切り後の未取得untracked変更 = %v", err)
	}
}

func TestDiffTextForPathsTrackedAtCapWithoutMissingChangeSucceeds(t *testing.T) {
	root, baseline := newTriggerRepo(t, map[string]string{
		"glm-worker/internal/runner/call.go": "package runner\n",
		"glm-worker/internal/state/stats.go": "package state\n",
	})
	writeTriggerFile(t, root, "glm-worker/internal/runner/call.go", "package runner\n\nvar callSite = newProcessGroupCmd\n")
	writeTriggerFile(t, root, "glm-worker/internal/state/stats.go", "package state\n\nvar usage = Usage{Tokens: 1}\n")
	paths := []string{"glm-worker/internal/runner/call.go", "glm-worker/internal/state/stats.go"}
	diffOut := runTriggerGit(t, root, append([]string{"diff", "--no-renames", baseline, "--"}, paths...)...)

	text, err := DiffTextForPaths(root, baseline, paths, len(diffOut))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "newProcessGroupCmd") || !strings.Contains(text, "Usage{Tokens: 1}") {
		t.Fatalf("capちょうどで両tracked変更が取得できていません: size = %d", len(text))
	}
}
