package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagedRulesForbidPublicationHookBypassEvenWithUserGitAllow(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	entries := managedRulesFiles(t)
	userRules := filepath.Join(t.TempDir(), "user-git-allow.rules")
	if err := os.WriteFile(userRules, []byte(`prefix_rule(pattern=["git"], decision="allow")`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	combined := append([]string{userRules}, entries...)

	forbidden := [][]string{
		{"git", "push", "origin", "main"},
		{"git", "push", "--no-verify", "origin", "main"},
		{"git", "push", "origin", "main", "--no-verify"},
		{"git", "push", "origin", "HEAD:main", "--no-verify"},
		{"git", "-c", "core.hooksPath=/dev/null", "commit", "-m", "bypass"},
		{"git", "config", "core.hooksPath", "/dev/null"},
		{"git", "config", "--local", "core.hooksPath", "/dev/null"},
		{"git", "config", "--unset", "core.hooksPath"},
		{"git", "config", "--local", "--unset-all", "core.hooksPath"},
	}
	for _, argv := range forbidden {
		if execPolicyAllows(t, codexBin, combined, argv) {
			t.Fatalf("publication hook bypassがuser allowで許可されました: %v", argv)
		}
	}

	allowed := [][]string{
		{"glm-parent-action", "push-binding", "push-guard", "--publish"},
		{"git", "commit", "-m", "normal"},
		{"git", "config", "user.name", "tester"},
		{"git", "config", "--local", "user.email", "tester@example.invalid"},
	}
	for _, argv := range allowed {
		if !execPolicyAllows(t, codexBin, combined, argv) {
			t.Fatalf("通常Git操作までmanaged rulesが拒否しました: %v", argv)
		}
	}
}
