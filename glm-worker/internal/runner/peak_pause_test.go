package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/peakpause"
)

func allDayTestPause(t *testing.T) peakpause.Policy {
	t.Helper()
	policy, err := peakpause.Parse("UTC", "sun-sat@00:00-12:01;sun-sat@12:00-23:59;sun-sat@23:58-00:01")
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestPeakPauseRejectsGLMCommandBeforeLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	r := &ClaudeRunner{}
	r.config.PeakPause = allDayTestPause(t)
	cmd := newProcessGroupCmd("sh", "-c", "echo started > "+marker)
	err := r.runCommand(cmd)
	var paused *ScheduledPauseError
	if !errors.As(err, &paused) || !paused.ResumeAt.After(time.Now()) || paused.Started {
		t.Fatalf("scheduled pause error = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("GLM process was launched during pause window: %v", statErr)
	}
}

func TestPeakPauseRejectsProbeBeforeLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "probe-started")
	r := &ClaudeRunner{}
	r.config.PeakPause = allDayTestPause(t)
	cmd := newProcessGroupCmd("sh", "-c", "echo probe > "+marker)
	err := r.runProbeCommand(cmd, time.Now().Add(time.Minute))
	var paused *ScheduledPauseError
	if !errors.As(err, &paused) {
		t.Fatalf("scheduled probe pause error = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("probe launched during pause window: %v", statErr)
	}
}
