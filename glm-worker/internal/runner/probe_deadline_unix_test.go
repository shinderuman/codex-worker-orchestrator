//go:build unix

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProbeWithDeadlineTerminatesHungProcessGroup(t *testing.T) {
	r, _, _ := newProbeFixture(t)
	pidPath := filepath.Join(t.TempDir(), "probe.pid")
	commandPath := filepath.Join(t.TempDir(), "hanging-probe")
	script := `#!/bin/sh
trap '' TERM
echo $$ > "$GLM_PROBE_PID"
(
  trap '' TERM
  while :; do sleep 0.2; done
) &
while :; do sleep 0.2; done
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_PROBE_PID", pidPath)
	r.config.ClaudeBin = commandPath
	r.config.EnvAllowlist = append(r.config.EnvAllowlist, "GLM_PROBE_PID")

	deadline := time.Now().Add(2 * time.Second)
	_, err := r.ProbeWithDeadline("opus", deadline)
	if !errors.Is(err, ErrProbeDeadlineExceeded) {
		t.Fatalf("ErrProbeDeadlineExceededを期待: %v", err)
	}

	data, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("probe pidを読めません: %v", readErr)
	}
	pgid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("probe pidが不正です: %v", convErr)
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Kill(-pgid, syscall.Signal(0)) == nil {
		if !time.Now().Before(until) {
			t.Fatalf("deadline後もprobe process group %dが残っています", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
