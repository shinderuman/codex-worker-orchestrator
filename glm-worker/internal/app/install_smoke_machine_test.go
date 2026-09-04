package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newInstallSmokeEnv(t *testing.T) (config.AppConfig, *state.StateStore, string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	failFlagPath := filepath.Join(t.TempDir(), "fail")
	countPath := filepath.Join(t.TempDir(), "count")
	script := "#!/bin/sh\nset -eu\n" +
		"count=$(cat '" + countPath + "' 2>/dev/null || printf 0)\n" +
		"count=$((count + 1))\n" +
		"printf '%s\\n' \"$count\" >'" + countPath + "'\n" +
		"if [ -f '" + failFlagPath + "' ]; then\n" +
		"printf '%s\\n' 'install smoke stdout fixture'\n" +
		"printf '%s\\n' 'assertion failed: install smoke fixture' >&2\n" +
		"printf '%s\\n' 'SMOKE_TOKEN=fixture-secret-value' >&2\n" +
		"printf '%s\\n' 'rejected sk-proj-AbC12345xY during install' >&2\n" +
		"printf '%s\\n' 'pull https://Aladdin:OpenSesame@github.example.invalid/org/repo.git' >&2\n" +
		"printf '%s\\n' 'ghp_AbC1defGHI456jklMNO789pqrSTU rejected' >&2\n" +
		"printf '%s\\n' 'AKIAIOSFODNN7EXAMPLE rejected' >&2\n" +
		"printf '%s\\n' 'kept https://github.example.invalid/org/repo.git and uuid 3f2504e0-4f89-11d3-9a0c-0305e82c3301' >&2\n" +
		"exit 1\n" +
		"fi\n"
	writeInstallSmokeScript(t, repoRoot, script)
	cfg, st := newInstallSmokeStateEnv(t, repoRoot)
	return cfg, st, failFlagPath, countPath
}

func newInstallSmokeScriptEnv(t *testing.T, script string) (config.AppConfig, *state.StateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	writeInstallSmokeScript(t, repoRoot, script)
	return newInstallSmokeStateEnv(t, repoRoot)
}

func newInstallSmokeStateEnv(t *testing.T, repoRoot string) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  config.RepoHashFor(repoRoot),
		RepoShort: config.RepoHashFor(repoRoot)[:12],
		StateBase: filepath.Join(t.TempDir(), "state"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func writeInstallSmokeScript(t *testing.T, repoRoot, script string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoRoot, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tests", "install_smoke.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func smokeInvocationCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return count
}
