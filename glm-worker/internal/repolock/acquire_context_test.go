package repolock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireContextStopsWhenLockRemainsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	lock, err := AcquireContext(ctx, path)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("AcquireContext acquired a held lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcquireContext error = %v want deadline exceeded", err)
	}
}
