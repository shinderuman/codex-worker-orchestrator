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
	if err := os.MkdirAll(filepath.Join(repoRoot, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\n" +
		"count=$(cat '" + countPath + "' 2>/dev/null || printf 0)\n" +
		"count=$((count + 1))\n" +
		"printf '%s\\n' \"$count\" >'" + countPath + "'\n" +
		"if [ -f '" + failFlagPath + "' ]; then exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(repoRoot, "tests", "install_smoke.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
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
	return cfg, st, failFlagPath, countPath
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
