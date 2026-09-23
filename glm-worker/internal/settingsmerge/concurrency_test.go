package settingsmerge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsTransactionRejectsConcurrentUnmanagedTargetEdit(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	writeTestFile(t, targetPath, `{"user_key":"initial"}`)

	targetSnapshot, err := captureMergeTransactionFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	overridePath := statePathFor(targetPath)
	managedPath := ManagedStatePath(targetPath)
	overrideSnapshot, err := captureMergeTransactionFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	managedSnapshot, err := captureMergeTransactionFile(managedPath)
	if err != nil {
		t.Fatal(err)
	}
	plans := []plannedWrite{{
		path: targetPath,
		data: []byte("{\n  \"managed_key\": \"new\",\n  \"user_key\": \"initial\"\n}\n"),
		mode: 0o600,
	}}
	inputs := map[string]fileRestore{
		targetPath:   targetSnapshot,
		overridePath: overrideSnapshot,
		managedPath:  managedSnapshot,
	}
	concurrent := []byte(`{"user_key":"concurrent"}`)
	if err := os.WriteFile(targetPath, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitRecoverableTransaction(targetPath, plans, inputs, writeAtomic); err == nil {
		t.Fatal("expected concurrent edit rejection")
	}
	if actual := readTestFile(t, targetPath); !bytes.Equal(actual, concurrent) {
		t.Fatalf("concurrent edit was lost: %s", actual)
	}
	if _, err := os.Stat(mergeTransactionPath(targetPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal exists after pre-mutation rejection: %v", err)
	}
}

func TestSettingsTransactionRollbackPreservesPostWriteEdit(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	writeTestFile(t, targetPath, `{"user_key":"initial"}`)
	targetSnapshot, err := captureMergeTransactionFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	managedPath := ManagedStatePath(targetPath)
	managedSnapshot, err := captureMergeTransactionFile(managedPath)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]fileRestore{targetPath: targetSnapshot, managedPath: managedSnapshot}
	plans := []plannedWrite{
		{path: targetPath, data: []byte("{\n  \"managed_key\": \"new\",\n  \"user_key\": \"initial\"\n}\n"), mode: 0o600},
		{path: managedPath, data: []byte("{\n  \"version\": 1,\n  \"values\": []\n}\n"), mode: 0o600},
	}
	concurrent := []byte(`{"user_key":"concurrent-after-write"}`)
	failure := errors.New("injected state write failure")
	writer := func(path string, data []byte, mode os.FileMode) error {
		if path == managedPath {
			if err := os.WriteFile(targetPath, concurrent, 0o600); err != nil {
				return err
			}
			return failure
		}
		return writeAtomic(path, data, mode)
	}
	err = commitRecoverableTransaction(targetPath, plans, inputs, writer)
	if !errors.Is(err, failure) {
		t.Fatalf("expected state write failure, got %v", err)
	}
	if actual := readTestFile(t, targetPath); !bytes.Equal(actual, concurrent) {
		t.Fatalf("rollback overwrote concurrent edit: %s", actual)
	}
	if _, err := os.Stat(mergeTransactionPath(targetPath)); err != nil {
		t.Fatalf("journal missing after unresolved rollback: %v", err)
	}
}

func TestSettingsMergeLockSerializesSameTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	first, err := acquireSettingsMergeLock(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		second, err := acquireSettingsMergeLock(targetPath)
		if err == nil {
			err = second.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second merge lock did not wait: %v", err)
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
		t.Fatal("second merge lock did not proceed after release")
	}
}
