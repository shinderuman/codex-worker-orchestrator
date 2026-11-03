//go:build unix

package observationexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestIsolatedGoModuleCacheContextHonorsDeadline(t *testing.T) {
	binDir := t.TempDir()
	goPath := filepath.Join(binDir, "go")
	marker := filepath.Join(t.TempDir(), "module-cache-pid")
	t.Setenv("OBSERVATION_CACHE_PID", marker)
	if err := execfile.Write(goPath, []byte("#!/bin/sh\nprintf '%s' \"$$\" > \"$OBSERVATION_CACHE_PID\"\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := isolatedGoModuleCacheContext(ctx); !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "not confirmed") {
		t.Fatal("deadline-bound module cache lookup unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 12*time.Second {
		t.Fatalf("module cache lookup ignored context deadline: %s", elapsed)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("module cache process did not start: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid module cache process PID: %q %v", data, err)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("module cache process remains after deadline: %d %v", pid, err)
	}
}
