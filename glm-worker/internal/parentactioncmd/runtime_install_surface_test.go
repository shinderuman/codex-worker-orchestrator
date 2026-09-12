package parentactioncmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func TestVerifyRuntimeManagedSurfaceChecksArtifactsOutsideTaskDiff(t *testing.T) {
	cfg, installedInstruction, _, _ := newRuntimeManagedSurfaceFixture(t)
	paths := []string{"glm-worker/internal/app/app.go"}
	if err := verifyRuntimeMergedConfigFiles(cfg, paths); err != nil {
		t.Fatalf("matching complete managed surface rejected: %v", err)
	}
	if err := os.WriteFile(installedInstruction, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeMergedConfigFiles(cfg, paths); err == nil {
		t.Fatal("runtime-only diff accepted stale unchanged managed Codex artifact")
	}
}

func TestVerifyRuntimeManagedSurfaceChecksClaudeAndAllRepositoryCLIs(t *testing.T) {
	t.Run("claude drift", func(t *testing.T) {
		cfg, _, claudeSettings, _ := newRuntimeManagedSurfaceFixture(t)
		if err := os.WriteFile(claudeSettings, []byte(`{"env":{"REPOSITORY":"stale"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyRuntimeMergedConfigFiles(cfg, []string{"install.sh"}); err == nil {
			t.Fatal("stale unchanged Claude managed settings were accepted")
		}
	})

	t.Run("secondary CLI drift", func(t *testing.T) {
		cfg, _, _, binDir := newRuntimeManagedSurfaceFixture(t)
		if err := os.WriteFile(filepath.Join(binDir, "harnesslint"), []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := verifyRuntimeMergedConfigFiles(cfg, []string{"glm-worker/internal/app/app.go"}); err == nil {
			t.Fatal("stale secondary repository CLI was accepted")
		}
	})
}

func newRuntimeManagedSurfaceFixture(t *testing.T) (config.AppConfig, string, string, string) {
	t.Helper()
	repo := t.TempDir()
	codexDir := t.TempDir()
	claudeDir := t.TempDir()
	binDir := t.TempDir()
	buildDir := t.TempDir()

	writeRuntimeSurfaceFile(t, repo, "codex/AGENTS.md", "# agents\n", 0o644)
	writeRuntimeSurfaceFile(t, repo, "codex/instructions/example.md", "current\n", 0o644)
	writeRuntimeSurfaceFile(t, repo, "codex/rules/glm-worker.rules", "prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n", 0o644)
	writeRuntimeSurfaceFile(t, repo, "codex/glm-worker/prompts/WORKER.md", "worker\n", 0o644)
	writeRuntimeSurfaceFile(t, repo, "codex/config-managed.toml", "background_terminal_max_timeout = 21600000\n", 0o644)
	if err := codexinstall.Install(repo, codexDir, io.Discard); err != nil {
		t.Fatal(err)
	}

	managedClaude := filepath.Join(repo, "claude", "settings-managed.json")
	writeRuntimeSurfaceFile(t, repo, "claude/settings-managed.json", `{"env":{"REPOSITORY":"managed"}}`, 0o644)
	claudeSettings := filepath.Join(claudeDir, "settings.json")
	if _, err := settingsmerge.MergeFiles(claudeSettings, managedClaude, ""); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"glm-worker", "glm-parent-action", "glm-codex-context", "commentlint", "harnesslint"} {
		writeRuntimeSurfaceFile(t, buildDir, name, "#!/bin/sh\nexit 0\n", 0o755)
	}
	if _, err := cliinstall.Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := config.AppConfig{
		RepoRoot:           repo,
		CodexConfigDir:     codexDir,
		ClaudeConfigDir:    claudeDir,
		ClaudeSettingsPath: claudeSettings,
	}
	return cfg, filepath.Join(codexDir, "instructions", "example.md"), claudeSettings, binDir
}

func writeRuntimeSurfaceFile(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
