package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func managedRulesFiles(t *testing.T) []string {
	t.Helper()
	root := scenarioRepoRoot(t)
	rulesDir := filepath.Join(root, "codex", "rules")
	entries, err := filepath.Glob(filepath.Join(rulesDir, "*.rules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("管理rules fileがありません: %s", rulesDir)
	}
	sort.Strings(entries)
	return entries
}

func execPolicyAllows(t *testing.T, codexBin string, entries []string, argv []string) bool {
	t.Helper()
	allowed, err := execPolicyCombinedAllow(codexBin, entries, argv)
	if err != nil {
		t.Fatalf("codex execpolicy check(%v, rules=%v)が失敗しました: %v", argv, entries, err)
	}
	return allowed
}

func execPolicyCombinedAllow(codexBin string, entries []string, argv []string) (bool, error) {
	args := make([]string, 0, 3+len(entries)*2+len(argv))
	args = append(args, "execpolicy", "check")
	for _, rulesFile := range entries {
		args = append(args, "--rules", rulesFile)
	}
	args = append(args, "--")
	args = append(args, argv...)
	out, err := exec.Command(codexBin, args...).Output()
	if err != nil {
		return false, err
	}
	var decision struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(out, &decision); err != nil {
		return false, fmt.Errorf("combined decisionの出力がJSONではありません: %w: %s", err, out)
	}
	return decision.Decision == "allow", nil
}

func TestManagedRulesAllowOnlyQualityGateEntrypoint(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	entries := managedRulesFiles(t)

	cases := []struct {
		argv      []string
		wantAllow bool
	}{
		{argv: []string{"glm-worker", "--quality-gate", "go-test"}, wantAllow: true},
		{argv: []string{"glm-worker", "--quality-gate", "go-test-race"}, wantAllow: true},
		{argv: []string{"go", "test", "./..."}, wantAllow: false},
		{argv: []string{"go", "test", "./...", "-exec", "/bin/sh"}, wantAllow: false},
		{argv: []string{"go", "test", "-race", "./...", "-exec", "/bin/sh"}, wantAllow: false},
		{argv: []string{"go", "test", "-exec", "/bin/sh", "./..."}, wantAllow: false},
	}

	for _, tc := range cases {
		if allowed := execPolicyAllows(t, codexBin, entries, tc.argv); allowed != tc.wantAllow {
			t.Fatalf("管理rulesの判定が想定と異なります: argv=%v wantAllow=%v gotAllow=%v", tc.argv, tc.wantAllow, allowed)
		}
	}
}

func TestManagedRulesAllowInstalledGLMRuntimeCommands(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	entries := managedRulesFiles(t)

	cases := []struct {
		argv      []string
		wantAllow bool
	}{
		{argv: []string{"glm-parent-action", "start"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "prepare", "decision"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "prepare", "fix"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "prepare", "start-milestones"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "prepare", "revise-milestones"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "decision", "token"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "fix", "token"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "fix", "token", "--origin", "codex-review"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "fix", "token", "--origin", "glm-reviewer", "--accepted-scope", "current-diff"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "fix", "token", "--origin", "metadata-repair", "--accepted-scope", "current-diff", "--approval-only"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "start-milestones", "token"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "revise-milestones", "token"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "no-go"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "accept"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "resume"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "finalize-check", "go-test"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "finalize-check", "go-test-race"}, wantAllow: true},
		{argv: []string{"glm-parent-action", "made-up-action"}, wantAllow: true},
		{argv: []string{"glm-codex-context", "enable", "/repository"}, wantAllow: true},
		{argv: []string{"glm-codex-context", "disable", "/repository"}, wantAllow: true},
		{argv: []string{"glm-codex-context", "status"}, wantAllow: true},
		{argv: []string{"git", "commit", "-m", "x"}, wantAllow: false},
		{argv: []string{"git", "commit", "--amend", "--no-edit"}, wantAllow: false},
		{argv: []string{"git", "push", "--force", "origin", "main"}, wantAllow: false},
		{argv: []string{"git", "push"}, wantAllow: false},
		{argv: []string{"git", "switch", "-c"}, wantAllow: false},
		{argv: []string{"unknown-parent-executable", "--flag"}, wantAllow: false},
	}

	for _, tc := range cases {
		if allowed := execPolicyAllows(t, codexBin, entries, tc.argv); allowed != tc.wantAllow {
			t.Fatalf("管理rulesの判定が想定と異なります: argv=%v wantAllow=%v gotAllow=%v", tc.argv, tc.wantAllow, allowed)
		}
	}
}

func TestManagedRulesLeaveGitCommitToUserPolicy(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	entries := managedRulesFiles(t)
	userRules := filepath.Join(t.TempDir(), "user-default.rules")
	userRule := `prefix_rule(pattern=["git", "commit"], decision="allow")`
	if err := os.WriteFile(userRules, []byte(userRule+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	combined := append([]string{userRules}, entries...)
	argv := []string{"git", "commit", "-m", "x"}
	if execPolicyAllows(t, codexBin, entries, argv) {
		t.Fatal("管理rules単独でgit commitをallowしています")
	}
	if !execPolicyAllows(t, codexBin, combined, argv) {
		t.Fatal("ユーザーrulesとの併用でgit commitがallowになりません")
	}
}

func TestExecPolicyCombinedCheckFailsClosed(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	entries := managedRulesFiles(t)
	badDir := t.TempDir()
	malformedRules := filepath.Join(badDir, "malformed.rules")
	if err := os.WriteFile(malformedRules, []byte("this is not valid rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubCodex := filepath.Join(badDir, "codex")
	stubScript := "#!/bin/sh\nprintf 'not json\\n'\n"
	if err := os.WriteFile(stubCodex, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	argv := []string{"glm-worker", "--quality-gate", "go-test"}
	cases := []struct {
		name     string
		codexBin string
		entries  []string
	}{
		{name: "malformed-rules-file", codexBin: codexBin, entries: append([]string{malformedRules}, entries...)},
		{name: "missing-rules-file", codexBin: codexBin, entries: append([]string{filepath.Join(badDir, "missing.rules")}, entries...)},
		{name: "malformed-output", codexBin: stubCodex, entries: entries},
	}
	for _, tc := range cases {
		allowed, err := execPolicyCombinedAllow(tc.codexBin, tc.entries, argv)
		if err == nil {
			t.Fatalf("%s: combined checkがerrorを返しませんでした", tc.name)
		}
		if allowed {
			t.Fatalf("%s: combined checkがallowへ縮退しています", tc.name)
		}
	}
}

func TestManagedRulesDirectoryHasNoDirectGoTestRule(t *testing.T) {
	for _, rulesFile := range managedRulesFiles(t) {
		data, err := os.ReadFile(rulesFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"go", "test"`) {
			t.Fatalf("管理rules fileが直接go testをallowしています: %s", rulesFile)
		}
	}
}
