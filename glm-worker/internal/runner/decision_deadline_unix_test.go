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

func TestDecideDeadlineTerminatesHungProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "decision.pid")
	script := `#!/bin/sh
trap '' TERM
echo $$ > "$GLM_DECIDE_PID"
(
  trap '' TERM
  while :; do sleep 0.2; done
) &
while :; do sleep 0.2; done
`
	r := newDecisionRunner(t, script)
	t.Setenv("GLM_DECIDE_PID", pidPath)
	r.config.EnvAllowlist = append(r.config.EnvAllowlist, "GLM_DECIDE_PID")
	r.decisionTimeout = 2 * time.Second

	_, err := r.Decide("opus", "low", "schema-json", "prompt-json")
	var callFailure *DecisionCallError
	if !errors.As(err, &callFailure) || callFailure.Reason != "deadline-exceeded" {
		t.Fatalf("deadline-exceededを期待: %v", err)
	}

	data, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("decision pidを読めません: %v", readErr)
	}
	pgid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("decision pidが不正です: %v", convErr)
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Kill(-pgid, syscall.Signal(0)) == nil {
		if !time.Now().Before(until) {
			t.Fatalf("deadline後もdecision process group %dが残っています", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
