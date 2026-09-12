package codexinstall

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyTracksCanonicalManagedFilesAndConfig(t *testing.T) {
	repo := t.TempDir()
	codexDir := t.TempDir()
	writeVerifyCodexSource(t, repo, "codex/AGENTS.md", "# agents\n")
	writeVerifyCodexSource(t, repo, "codex/instructions/example.md", "current\n")
	writeVerifyCodexSource(t, repo, "codex/rules/glm-worker.rules", "prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n")
	writeVerifyCodexSource(t, repo, "codex/glm-worker/prompts/WORKER.md", "worker\n")
	writeVerifyCodexSource(t, repo, "codex/config-managed.toml", "background_terminal_max_timeout = 21600000\n")

	if err := Install(repo, codexDir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, codexDir); err != nil {
		t.Fatalf("matching installation rejected: %v", err)
	}

	instruction := filepath.Join(codexDir, "instructions", "example.md")
	if err := os.WriteFile(instruction, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, codexDir); err == nil {
		t.Fatal("managed file drift was accepted")
	}
	if err := os.WriteFile(instruction, []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("background_terminal_max_timeout = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, codexDir); err == nil {
		t.Fatal("managed config drift was accepted")
	}
}

func TestVerifyFollowsRetiredFileOwnershipSemantics(t *testing.T) {
	repo := t.TempDir()
	codexDir := t.TempDir()
	writeVerifyCodexSource(t, repo, "codex/AGENTS.md", "# agents\n")
	writeVerifyCodexSource(t, repo, "codex/instructions/retired.md", "managed\n")
	writeVerifyCodexSource(t, repo, "codex/rules/glm-worker.rules", "prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n")
	writeVerifyCodexSource(t, repo, "codex/glm-worker/prompts/WORKER.md", "worker\n")
	writeVerifyCodexSource(t, repo, "codex/config-managed.toml", "background_terminal_max_timeout = 21600000\n")
	if err := Install(repo, codexDir, io.Discard); err != nil {
		t.Fatal(err)
	}

	installed := filepath.Join(codexDir, "instructions", "retired.md")
	if err := os.WriteFile(installed, []byte("user modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "codex", "instructions", "retired.md")); err != nil {
		t.Fatal(err)
	}
	if err := Install(repo, codexDir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Verify(repo, codexDir); err != nil {
		t.Fatalf("preserved user-modified retired file rejected: %v", err)
	}
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user modified\n" {
		t.Fatalf("retired user file changed: %q", data)
	}
}

func writeVerifyCodexSource(t *testing.T, repo, relative, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
