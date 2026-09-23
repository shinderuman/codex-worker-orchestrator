package codexinstall

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyRejectsConfigEditAfterPreparation(t *testing.T) {
	repo := initConcurrentFixtureRepo(t)
	dest := t.TempDir()
	configPath := filepath.Join(dest, "config.toml")
	writeConcurrentFile(t, configPath, []byte("local_key = \"initial\"\n"))
	prep, err := prepareInstall(repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("local_key = \"changed-during-install\"\n")
	writeConcurrentFile(t, configPath, changed)
	if err := applyInstall(prep, io.Discard); err == nil || !strings.Contains(err.Error(), "config changed after preparation") {
		t.Fatalf("expected config concurrency rejection, got %v", err)
	}
	assertConcurrentBytes(t, configPath, changed)
	if _, err := os.Stat(statePath(dest)); !os.IsNotExist(err) {
		t.Fatalf("state written after rejection: %v", err)
	}
}

func TestApplyRejectsManagedFileEditAfterPreparation(t *testing.T) {
	repo := initConcurrentFixtureRepo(t)
	dest := t.TempDir()
	runConcurrentInstall(t, repo, dest)
	source := filepath.Join(repo, "codex", "instructions", "test.md")
	target := filepath.Join(dest, "instructions", "test.md")
	writeConcurrentFile(t, source, []byte("# source v2\n"))
	prep, err := prepareInstall(repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	user := []byte("# user edit after prepare\n")
	writeConcurrentFile(t, target, user)
	if err := applyInstall(prep, io.Discard); err == nil || !strings.Contains(err.Error(), "changed after preparation") {
		t.Fatalf("expected managed-file concurrency rejection, got %v", err)
	}
	assertConcurrentBytes(t, target, user)
}

func TestApplyRejectsObsoleteFileEditAfterPreparation(t *testing.T) {
	repo := initConcurrentFixtureRepo(t)
	dest := t.TempDir()
	runConcurrentInstall(t, repo, dest)
	source := filepath.Join(repo, "codex", "instructions", "test.md")
	target := filepath.Join(dest, "instructions", "test.md")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	prep, err := prepareInstall(repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	user := []byte("# user retained retired path\n")
	writeConcurrentFile(t, target, user)
	if err := applyInstall(prep, io.Discard); err == nil || !strings.Contains(err.Error(), "changed after preparation") {
		t.Fatalf("expected obsolete-file concurrency rejection, got %v", err)
	}
	assertConcurrentBytes(t, target, user)
}

func TestApplyRejectsStateEditAfterPreparation(t *testing.T) {
	repo := initConcurrentFixtureRepo(t)
	dest := t.TempDir()
	runConcurrentInstall(t, repo, dest)
	writeConcurrentFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# source v2\n"))
	prep, err := prepareInstall(repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	stateFile := statePath(dest)
	original := readConcurrentFile(t, stateFile)
	changed := append(append([]byte(nil), original...), '\n')
	writeConcurrentFile(t, stateFile, changed)
	if err := applyInstall(prep, io.Discard); err == nil || !strings.Contains(err.Error(), "state changed after preparation") {
		t.Fatalf("expected state concurrency rejection, got %v", err)
	}
	assertConcurrentBytes(t, stateFile, changed)
}

func TestRollbackPreservesEditMadeAfterInstallerWrite(t *testing.T) {
	repo := initConcurrentFixtureRepo(t)
	dest := t.TempDir()
	configPath := filepath.Join(dest, "config.toml")
	writeConcurrentFile(t, configPath, []byte("local_key = \"initial\"\n"))
	runConcurrentInstall(t, repo, dest)
	oldTarget := readConcurrentFile(t, filepath.Join(dest, "instructions", "test.md"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# source v2\n"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))
	prep, err := prepareInstall(repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	stateFailure := errors.New("state commit failed")
	userConfig := []byte(managedConfigKey + " = 21600001\nlocal_key = \"user-after-write\"\n")
	err = applyInstallWithStateWriter(prep, io.Discard, func(string, installState) error {
		writeConcurrentFile(t, configPath, userConfig)
		return stateFailure
	})
	if !errors.Is(err, stateFailure) {
		t.Fatalf("expected state failure, got %v", err)
	}
	assertConcurrentBytes(t, configPath, userConfig)
	assertConcurrentBytes(t, filepath.Join(dest, "instructions", "test.md"), oldTarget)
}

func TestCodexInstallLockSerializesSameDestination(t *testing.T) {
	dest := t.TempDir()
	first, err := acquireInstallLock(dest)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		second, err := acquireInstallLock(dest)
		if err == nil {
			err = second.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second lock did not wait: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not proceed after release")
	}
}

func initConcurrentFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeConcurrentFile(t, filepath.Join(repo, "codex", "AGENTS.md"), []byte("# parent rules\n"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# source v1\n"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "rules", "glm-worker.rules"), []byte("prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "glm-worker", "prompts", "WORKER.md"), []byte("# worker\n"))
	writeConcurrentFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600000\n"))
	return repo
}

func runConcurrentInstall(t *testing.T, repo, dest string) {
	t.Helper()
	var out bytes.Buffer
	if err := Install(repo, dest, &out); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
}
func writeConcurrentFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
func readConcurrentFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func assertConcurrentBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got := readConcurrentFile(t, path)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s changed\nwant %q\ngot  %q", path, want, got)
	}
}
