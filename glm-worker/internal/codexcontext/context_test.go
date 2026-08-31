package codexcontext

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableDisableManagedProjectConfig(t *testing.T) {
	repo := initTestRepo(t)

	result := runAction(t, "enable", repo)
	if result.Status != "enabled" || !result.GitExcluded || !result.RequiresNewThread || result.DesktopRestart {
		t.Fatalf("unexpected enable result: %+v", result)
	}
	configPath := filepath.Join(repo, filepath.FromSlash(ProjectConfigRelativePath))
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, ManagedConfigContent()) {
		t.Fatalf("unexpected managed config:\n%s", content)
	}
	for _, setting := range []string{
		"include_apps_instructions = false",
		"include_collaboration_mode_instructions = false",
		"include_instructions = false",
		"apps = false",
		"plugins = false",
		"default_exec_yield_time_ms = 21600000",
		"root_agent_usage_hint_text = \"\"",
		"multi_agent_mode_hint_text = \"\"",
	} {
		if !strings.Contains(string(content), setting) {
			t.Fatalf("managed config is missing %q:\n%s", setting, content)
		}
	}
	if status := gitOutput(t, repo, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("managed project config polluted git status: %q", status)
	}

	second := runAction(t, "enable", repo)
	if second.Status != "enabled" || !second.GitExcluded {
		t.Fatalf("second enable is not idempotent: %+v", second)
	}

	status := runAction(t, "status", repo)
	if status.Status != "enabled" || !status.GitExcluded || status.RequiresNewThread {
		t.Fatalf("unexpected status result: %+v", status)
	}

	disabled := runAction(t, "disable", repo)
	if disabled.Status != "disabled" || disabled.GitExcluded || !disabled.RequiresNewThread {
		t.Fatalf("unexpected disable result: %+v", disabled)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("managed config still exists after disable: %v", err)
	}
	if status := gitOutput(t, repo, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("disable polluted git status: %q", status)
	}
}

func TestEnableRefusesExistingProjectConfig(t *testing.T) {
	repo := initTestRepo(t)
	configPath := filepath.Join(repo, filepath.FromSlash(ProjectConfigRelativePath))
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("[skills]\ninclude_instructions = true\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := Run([]string{"enable", repo}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite existing .codex/config.toml") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	content, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("existing project config changed: %q", content)
	}
	status := runAction(t, "status", repo)
	if status.Status != "conflict" {
		t.Fatalf("expected conflict status, got %+v", status)
	}
}

func TestPreexistingGitExcludeIsPreserved(t *testing.T) {
	repo := initTestRepo(t)
	excludePath := gitOutput(t, repo, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(repo, excludePath)
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# user rule\n" + excludePattern + "\n"
	if err := os.WriteFile(excludePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	runAction(t, "enable", repo)
	runAction(t, "disable", repo)

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("preexisting exclude changed:\nwant %q\ngot  %q", original, string(content))
	}
}

func runAction(t *testing.T, action, repo string) Result {
	t.Helper()
	var stdout bytes.Buffer
	if err := Run([]string{action, repo}, &stdout); err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode %s output %q: %v", action, stdout.String(), err)
	}
	return result
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	command := exec.Command("git", "init", "-q", repo)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return repo
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}
