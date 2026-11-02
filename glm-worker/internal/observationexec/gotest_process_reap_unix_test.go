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
)

func TestObservationPreflightCancellationStopsAndReapsDescendant(t *testing.T) {
	dir := t.TempDir()
	parentMarker := filepath.Join(dir, "parent-pid")
	childMarker := filepath.Join(dir, "child-pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := make(chan struct{})
	go func() {
		defer close(checked)
		deadline := time.After(5 * time.Second)
		for {
			if _, err := os.Stat(parentMarker); err == nil {
				if _, err := os.Stat(childMarker); err == nil {
					cancel()
					return
				}
			}
			select {
			case <-deadline:
				cancel()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	script := "printf '%s' \"$$\" > \"$1\"; /bin/sh -c 'printf \"%s\" \"$$\" > \"$1\"; exec sleep 30' sh \"$2\" & child=$!; trap 'wait \"$child\"; exit 0' TERM; wait \"$child\""
	started := time.Now()
	_, err := runObservationPreflightProcess(ctx, []string{"/bin/sh", "-c", script, "sh", parentMarker, childMarker})
	<-checked
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight cancellation = %v", err)
	}
	if strings.Contains(err.Error(), "wait/reap not confirmed") {
		t.Fatalf("preflight returned without confirmed owned-child wait: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 12*time.Second {
		t.Fatalf("preflight descendant cleanup exceeded bound: %s", elapsed)
	}
	for _, marker := range []string{parentMarker, childMarker} {
		data, readErr := os.ReadFile(marker)
		if readErr != nil {
			t.Fatalf("process never started (%s): %v", marker, readErr)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr != nil || pid <= 0 {
			t.Fatalf("invalid process PID (%s): %q %v", marker, data, parseErr)
		}
		if killErr := syscall.Kill(pid, 0); killErr != syscall.ESRCH {
			t.Fatalf("cancelled process %d remains (marker %s): %v", pid, marker, killErr)
		}
	}
}
