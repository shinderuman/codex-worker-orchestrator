package uninstallcmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/installlock"
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

func TestConcurrentInstallUninstallRejectsOverlapBeforeOwnershipChanges(t *testing.T) {
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

	install := func() error {
		return installlock.With(func(*os.File) error {
			if _, err := cliinstall.Install(buildDir, binDir); err != nil {
				return err
			}
			if err := codexinstall.Install(repo, codexDir, io.Discard); err != nil {
				return err
			}
			if _, err := settingsmerge.MergeFiles(claudePath, filepath.Join(repo, "claude", "settings-managed.json"), ""); err != nil {
				return err
			}
			cmd := exec.Command("sh", filepath.Join(repo, "scripts", "manage-pull-hook.sh"), "install", repo, filepath.Join(binDir, "glm-publication-guard"))
			return cmd.Run()
		})
	}
	if err := install(); err != nil {
		t.Fatalf("初回install: %v", err)
	}

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

	if err := install(); !errors.Is(err, installlock.ErrBusy) {
		t.Fatalf("競合したinstallが拒否されません: %v", err)
	}
	close(gate.release)
	select {
	case err := <-uninstalled:
		if err != nil {
			t.Fatalf("uninstall exited nonzero: %v\n%s", err, gate.output.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("uninstall did not finish after the gate was released")
	}

	guard := filepath.Join(binDir, "glm-publication-guard")
	codexManaged := filepath.Join(codexDir, "instructions", "test.md")
	claudeState := filepath.Join(home, ".claude", ".codex-worker-orchestrator", "settings.json.managed-settings-state.json")
	hook := filepath.Join(repo, ".git", "codex-worker-orchestrator", "hooks", "pre-push")
	for _, owned := range []string{guard, codexManaged, claudeState, hook} {
		assertConcurrentRetireAbsent(t, owned)
	}
	assertConcurrentRetirePresent(t, filepath.Join(codexDir, "AGENTS.md"))
	if err := install(); err != nil {
		t.Fatalf("排他解除後の再install: %v", err)
	}
	for _, owned := range []string{guard, codexManaged, claudeState, hook} {
		assertConcurrentRetirePresent(t, owned)
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
