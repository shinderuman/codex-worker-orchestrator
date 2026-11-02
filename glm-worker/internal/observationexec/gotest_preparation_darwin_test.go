//go:build darwin

package observationexec

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinObservationCancelledPreflightDoesNotLaunchTarget(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "preflight-started")
	probe := filepath.Join(binDir, "sandbox-exec")
	script := "#!/bin/sh\nprintf %s \"$$\" > \"$OBSERVATION_PREFLIGHT_MARKER\"\nexec sleep 30\n"
	if err := os.WriteFile(probe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OBSERVATION_PREFLIGHT_MARKER", marker)
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) { t.Fatal(\"must not launch\") }\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		deadline := time.After(3 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				cancel()
				return
			}
			select {
			case <-deadline:
				cancel()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	started := time.Now()
	outcome := RunIsolatedGoTest(ctx, GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "preflight-cancel-darwin", DeadlineMS: 15000,
	})
	<-stopped
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceCancelled {
		t.Fatalf("cancelled Darwin preflight outcome = %+v", outcome)
	}
	if time.Since(started) > 12*time.Second {
		t.Fatalf("Darwin preflight did not stop in bounded time")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("preflight fixture never started: %v", err)
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil || pid <= 0 {
		t.Fatalf("invalid preflight process PID %q: %v", data, parseErr)
	}
	if err := syscall.Kill(pid, 0); err == nil || err != syscall.ESRCH {
		t.Fatalf("cancelled preflight process %d still exists: %v", pid, err)
	}
	if outcome.LogPath != "" {
		t.Fatalf("preflight failure created target result: %s", outcome.LogPath)
	}
}
