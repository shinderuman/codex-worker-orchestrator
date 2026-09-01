//go:build unix

package repolock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	if _, err := Acquire(path); !errors.Is(err, ErrRepoLockHeld) {
		t.Fatalf("2つ目のロック取得 error = %v, want ErrRepoLockHeld", err)
	}
}

func TestAcquireReusableAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("Close後の再取得に失敗しました: %v", err)
	}
	_ = second.Close()
}
