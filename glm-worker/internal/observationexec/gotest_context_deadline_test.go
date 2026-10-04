package observationexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsolatedGoModuleCacheContextHonorsDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	binDir := t.TempDir()
	goPath := filepath.Join(binDir, "go")
	if err := os.WriteFile(goPath, []byte("#!/bin/sh\nsleep 5\nprintf '/tmp/unused-cache\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := isolatedGoModuleCacheContext(ctx); err == nil {
		t.Fatal("deadline-bound module cache lookup unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("module cache lookup ignored context deadline: %s", elapsed)
	}
}
