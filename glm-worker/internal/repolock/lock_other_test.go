//go:build !unix

package repolock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNonUnixAcquireDistinguishesContentionFromCreationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if _, err := Acquire(path); !errors.Is(err, ErrRepoLockHeld) {
		t.Fatalf("existing lock error = %v", err)
	}
	missingParent := filepath.Join(t.TempDir(), "missing", "lock")
	if _, err := Acquire(missingParent); !errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrRepoLockHeld) {
		t.Fatalf("missing directory classified as contention: %v", err)
	}
}

func TestNonUnixAcquireWaitReturnsCreationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "lock")
	done := make(chan error, 1)
	go func() {
		lock, err := AcquireWait(path)
		if lock != nil {
			_ = lock.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrRepoLockHeld) {
			t.Fatalf("missing directory error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AcquireWait retried a non-contention error")
	}
}
