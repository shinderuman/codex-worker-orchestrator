package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareScheduledPauseStop(t *testing.T, resumeAt time.Time) (*state.StateStore, *runner.ScheduledPauseError) {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{}
	checkpoint.Stage = state.ResumeStageReview
	checkpoint.Phase = "reviewer-1"
	checkpoint.Role = state.ReviewerRole
	checkpoint.Model = "sonnet"
	checkpoint.SetStopKind(state.ResumeStopScheduledPause)
	checkpoint.ScheduledResumeAt = resumeAt.Format(time.RFC3339)
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	paused := &runner.ScheduledPauseError{}
	paused.Phase = checkpoint.Phase
	paused.TaskID = taskID
	paused.RepoRoot = cfg.RepoRoot
	paused.ResumeAt = resumeAt
	return st, paused
}

func TestScheduledPauseSelfResumeAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	resumeAt := now.Add(time.Hour)
	st, paused := prepareScheduledPauseStop(t, resumeAt)
	controller := runner.NewStopController()
	resumed := 0
	waits := 0
	err := executeZaiSelfResumeLoop(st, controller, func() error {
		return paused
	}, func() error {
		resumed++
		return nil
	}, func() time.Time { return now }, func(at time.Time, _ *runner.StopController) bool {
		waits++
		if !at.Equal(resumeAt) {
			t.Fatalf("wake boundary = %v want %v", at, resumeAt)
		}
		now = at
		return false
	})
	if err != nil || resumed != 1 || waits != 1 {
		t.Fatalf("scheduled resume err=%v resumed=%d waits=%d", err, resumed, waits)
	}
}

func TestScheduledPauseWakeRejectsChangedCheckpoint(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	st, paused := prepareScheduledPauseStop(t, now.Add(time.Hour))
	err := waitForScheduledPause(st, runner.NewStopController(), paused, func() time.Time { return now }, func(_ time.Time, _ *runner.StopController) bool {
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Fatal(err)
		}
		checkpoint.ScheduledResumeAt = now.Add(2 * time.Hour).Format(time.RFC3339)
		if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
			t.Fatal(err)
		}
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "scheduled-pause checkpoint") {
		t.Fatalf("changed checkpoint must fail closed: %v", err)
	}
}

func TestScheduledPauseWaitInterruptionRetainsScheduledState(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	st, paused := prepareScheduledPauseStop(t, now.Add(time.Hour))
	err := waitForScheduledPause(st, runner.NewStopController(), paused, func() time.Time { return now }, func(_ time.Time, _ *runner.StopController) bool {
		return true
	})
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatalf("interrupted pause wait = %v", err)
	}
	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if st.TaskStatus() != state.TaskStatusScheduledPause || checkpoint.StopKind != state.ResumeStopScheduledPause {
		t.Fatalf("pause wait interruption changed durable state: %s/%s", st.TaskStatus(), checkpoint.StopKind)
	}
}
