package uninstallcmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

type concurrentRetireGate struct {
	output  bytes.Buffer
	reached chan struct{}
	release chan struct{}
}

func (gate *concurrentRetireGate) Write(data []byte) (int, error) {
	count, err := gate.output.Write(data)
	if bytes.Contains(data, []byte("uninstall Claude: completed or unchanged")) {
		close(gate.reached)
		<-gate.release
	}
	return count, err
}

func TestConcurrentInstallUninstallCanBothSucceedWithSplitOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SETTINGS_FILE", filepath.Join(home, ".claude", "settings.json"))

	repo := createConcurrentRetireFixture(t)
	binDir := filepath.Join(home, ".local", "bin")
	codexDir := filepath.Join(home, ".codex")
	claudePath := filepath.Join(home, ".claude", "settings.json")
	buildDir := filepath.Join(home, "build")
	for _, name := range []string{
		"glm-worker", "glm-parent-action", "glm-publication-guard",
		"glm-codex-context", "commentlint", "harnesslint",
	} {
		writeConcurrentRetireFile(t, filepath.Join(buildDir, name), []byte(name+"-fixture"), 0o755)
	}
	writeConcurrentRetireFile(t, claudePath, []byte(`{"env":{"LOCAL":"keep"}}`), 0o600)
	writeConcurrentRetireFile(t, filepath.Join(codexDir, "AGENTS.md"), []byte("user-owned instruction\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(codexDir, "config.toml"), []byte("local_key = \"keep\"\n"), 0o644)

	// install.sh completes the CLI owner before starting the Codex owner.
	if _, err := cliinstall.Install(buildDir, binDir); err != nil {
		t.Fatalf("install CLI phase: %v", err)
	}

	// uninstall starts after install CLI, but pauses after retiring Claude
	// and before retiring CLI. Channels, not sleeps, control the interleaving.
	gate := &concurrentRetireGate{
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer func() {
		select {
		case <-gate.release:
		default:
			close(gate.release)
		}
	}()
	uninstalled := make(chan error, 1)
	go func() {
		uninstalled <- run(repo, binDir, codexDir, gate)
	}()
	select {
	case <-gate.reached:
	case <-time.After(20 * time.Second):
		t.Fatal("uninstall did not reach the post-Claude boundary")
	}

	// While uninstall is paused, install completes Codex, Claude and hook.
	if err := codexinstall.Install(repo, codexDir, io.Discard); err != nil {
		t.Fatalf("install Codex phase: %v", err)
	}
	if _, err := settingsmerge.MergeFiles(claudePath, filepath.Join(repo, "claude", "settings-managed.json"), ""); err != nil {
		t.Fatalf("install Claude phase: %v", err)
	}
	runConcurrentRetireCommand(t, "sh", filepath.Join(repo, "scripts", "manage-pull-hook.sh"),
		"install", repo, filepath.Join(binDir, "glm-publication-guard"))
	close(gate.release)
	select {
	case err := <-uninstalled:
		if err != nil {
			t.Fatalf("uninstall exited nonzero: %v\n%s", err, gate.output.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("uninstall did not finish after the gate was released")
	}

	// Both logical commands completed successfully, but only CLI was retired.
	// This is a reproducible known race, not an acceptance of split deployment.
	assertConcurrentRetireAbsent(t, filepath.Join(binDir, "glm-publication-guard"))
	assertConcurrentRetireAbsent(t, filepath.Join(binDir, ".codex-worker-orchestrator", "cli-install-state.json"))
	assertConcurrentRetirePresent(t, filepath.Join(codexDir, "codex-worker-orchestrator", "install-state.json"))
	assertConcurrentRetirePresent(t, filepath.Join(codexDir, "instructions", "test.md"))
	assertConcurrentRetirePresent(t, filepath.Join(home, ".claude", ".codex-worker-orchestrator", "settings.json.managed-settings-state.json"))
	hooksDir := filepath.Join(repo, ".git", "codex-worker-orchestrator", "hooks")
	assertConcurrentRetirePresent(t, filepath.Join(hooksDir, "glm-publication-guard.path"))
	assertConcurrentRetirePresent(t, filepath.Join(hooksDir, "pre-push"))
	gitHooksPath := strings.TrimSpace(runConcurrentRetireCommand(t, "git", "-C", repo, "config", "--local", "--get", "core.hooksPath"))
	if gitHooksPath != hooksDir {
		t.Fatalf("hook activation = %q, want %q", gitHooksPath, hooksDir)
	}
	assertConcurrentRetirePresent(t, filepath.Join(codexDir, "AGENTS.md"))
	if got, err := os.ReadFile(claudePath); err != nil || !bytes.Contains(got, []byte(`"MANAGED": "set"`)) {
		t.Fatalf("Claude installed value missing: %s, %v", got, err)
	}
}

func createConcurrentRetireFixture(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	writeConcurrentRetireFile(t, filepath.Join(repo, "codex", "AGENTS.md"), []byte("# rules\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# fixture\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(repo, "codex", "rules", "glm-worker.rules"),
		[]byte("prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(repo, "codex", "glm-worker", "prompts", "WORKER.md"),
		[]byte("# worker\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(repo, "codex", "config-managed.toml"),
		[]byte("background_terminal_max_timeout = 21600000\n"), 0o644)
	writeConcurrentRetireFile(t, filepath.Join(repo, "claude", "settings-managed.json"),
		[]byte(`{"env":{"MANAGED":"set"}}`), 0o644)

	script, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "manage-pull-hook.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeConcurrentRetireFile(t, filepath.Join(repo, "scripts", "manage-pull-hook.sh"), script, 0o755)
	for _, name := range []string{"post-merge", "pre-commit", "reference-transaction", "pre-push"} {
		writeConcurrentRetireFile(t, filepath.Join(repo, ".githooks", name),
			[]byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	runConcurrentRetireCommand(t, "git", "init", "-q", repo)
	runConcurrentRetireCommand(t, "git", "-C", repo, "add", "-A")
	runConcurrentRetireCommand(t, "git", "-C", repo, "-c", "user.name=fixture",
		"-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
	return repo
}

func writeConcurrentRetireFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func runConcurrentRetireCommand(t *testing.T, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", command, args, err, output)
	}
	return string(output)
}

func assertConcurrentRetirePresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected present %s: %v", path, err)
	}
}

func assertConcurrentRetireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", path, err)
	}
}

