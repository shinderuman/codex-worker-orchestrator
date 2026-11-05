//go:build unix

package runner

import (
	"errors"
	"testing"
	"time"
)

func TestPeakPauseStopsRunningProcessAtBoundary(t *testing.T) {
	start := time.Now()
	begin := start.Add(150 * time.Millisecond)
	resume := start.Add(time.Hour)
	r := &ClaudeRunner{
		pauseEvaluator: func(now time.Time) (bool, time.Time) {
			if now.Before(begin) {
				return false, begin
			}
			return true, resume
		},
	}
	command := newProcessGroupCmd("sh", "-c", "sleep 20")
	err := r.runCommand(command)
	var paused *ScheduledPauseError
	if !errors.As(err, &paused) || !paused.ResumeAt.Equal(resume) {
		t.Fatalf("running GLM command was not interrupted at boundary: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("scheduled stop exceeded safe interruption budget: %s", elapsed)
	}
}

func TestPeakPauseNaturalExitBeforeBoundary(t *testing.T) {
	start := time.Now()
	r := &ClaudeRunner{
		pauseEvaluator: func(time.Time) (bool, time.Time) {
			return false, start.Add(time.Second)
		},
	}
	if err := r.runCommand(newProcessGroupCmd("sh", "-c", "exit 0")); err != nil {
		t.Fatalf("natural exit incorrectly stopped: %v", err)
	}
}
