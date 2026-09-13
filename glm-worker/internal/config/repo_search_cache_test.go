package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadDerivesRepoSearchCacheRootFromWorkerHome(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "init", "--quiet", repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repository); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	home := t.TempDir()
	workerHome := filepath.Join(t.TempDir(), "worker-home")
	t.Setenv("HOME", home)
	t.Setenv("GLM_WORKER_HOME", workerHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SETTINGS_FILE", "")

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workerHome, "search")
	if loaded.RepoSearchCacheRoot != want {
		t.Fatalf("RepoSearchCacheRoot = %q, want %q", loaded.RepoSearchCacheRoot, want)
	}
}
