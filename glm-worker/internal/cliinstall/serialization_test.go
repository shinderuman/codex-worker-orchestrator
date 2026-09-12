package cliinstall

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

func TestInstallWaitsForSharedInstallerLock(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")

	lock, err := repolock.AcquireWait(filepath.Join(binDir, installLockFileName))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := Install(buildDir, binDir)
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("Install returned while shared installer lock was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Install did not resume after shared installer lock was released")
	}
}

func TestRetireCleansInterruptedStateTemp(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(statePathForTest(binDir))
	stale := filepath.Join(stateDir, ".cli-install-state-interrupted.tmp")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Retire(binDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state directory remains after stale temp cleanup: %v", err)
	}
}
