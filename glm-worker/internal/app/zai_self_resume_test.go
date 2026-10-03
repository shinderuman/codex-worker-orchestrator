package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareZaiSelfResumeStop(t *testing.T) (*state.StateStore, runner.ZaiRateLimitError) {
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
	resetAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339)
	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker,
		Phase: "worker-new",
		Role:  state.WorkerRole,
		Model: "test-model",
	}
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtRFC3339 = resetAt
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	return st, runner.ZaiRateLimitError{
		Phase:     checkpoint.Phase,
		TaskID:    taskID,
		RepoRoot:  cfg.RepoRoot,
		RepoShort: cfg.RepoShort,
		Limit: runner.ZaiFiveHourLimit{
			ResetAtRFC3339: resetAt,
		},
	}
}

func setZaiSelfResumeReset(t *testing.T, st *state.StateStore, limitErr *runner.ZaiRateLimitError, resetAt time.Time) {
	t.Helper()
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	value := resetAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	checkpoint.ResetAtRFC3339 = value
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	limitErr.Limit.ResetAtRFC3339 = value
}

func replaceZaiSelfResumeWait(t *testing.T, fn func(time.Time, *runner.StopController) bool) {
	t.Helper()
	previous := waitForZaiSelfResume
	waitForZaiSelfResume = fn
	t.Cleanup(func() { waitForZaiSelfResume = previous })
}

func replaceZaiSelfResumeNow(t *testing.T, fn func() time.Time) {
	t.Helper()
	previous := zaiSelfResumeNow
	zaiSelfResumeNow = fn
	t.Cleanup(func() { zaiSelfResumeNow = previous })
}

func TestExecuteZaiSelfResumeLoopRepeatsFiveHourStops(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	waits := 0
	replaceZaiSelfResumeWait(t, func(_ time.Time, _ *runner.StopController) bool {
		waits++
		return false
	})

	resumes := 0
	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return limitErr },
		func() error {
			resumes++
			if resumes == 1 {
				return limitErr
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if waits != 2 || resumes != 2 {
		t.Fatalf("self-resume loop waits=%d resumes=%d, want 2/2", waits, resumes)
	}
}

func TestExecuteZaiSelfResumeLoopDoesNotStackTenSecondsAfterLongCLIBackoff(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	setZaiSelfResumeReset(t, st, &limitErr, now.Add(-5*time.Second))
	replaceZaiSelfResumeNow(t, func() time.Time { return now })

	var waitTargets []time.Time
	replaceZaiSelfResumeWait(t, func(target time.Time, _ *runner.StopController) bool {
		waitTargets = append(waitTargets, target)
		if target.After(now) {
			now = target
		}
		return false
	})

	resumes := 0
	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return limitErr },
		func() error {
			resumes++
			if resumes == 1 {
				// Simulate Claude CLI spending longer than the outer 10s cadence in its own retry/backoff.
				now = now.Add(15 * time.Second)
				return limitErr
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resumes != 2 {
		t.Fatalf("resumes = %d want 2", resumes)
	}
	if len(waitTargets) != 1 {
		t.Fatalf("wait targets = %v; long CLI backoff must not receive an additional fixed 10s sleep", waitTargets)
	}
	want := time.Date(2026, 10, 3, 12, 0, 10, 0, time.UTC)
	if !waitTargets[0].Equal(want) {
		t.Fatalf("first retry target = %s want %s", waitTargets[0], want)
	}
}

func TestExecuteZaiSelfResumeLoopBoundsPostResetRetriesByWallClock(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	now := time.Date(2026, 10, 3, 12, 2, 1, 0, time.UTC)
	setZaiSelfResumeReset(t, st, &limitErr, time.Date(2026, 10, 3, 11, 59, 55, 0, time.UTC))
	replaceZaiSelfResumeNow(t, func() time.Time { return now })

	resumes := 0
	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return limitErr },
		func() error {
			resumes++
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "post-reset retry window exhausted") {
		t.Fatalf("retry-window error = %v", err)
	}
	if resumes != 0 {
		t.Fatalf("resumes = %d want 0 after retry window exhaustion", resumes)
	}
}

func TestExecuteZaiSelfResumeLoopUsesNewFutureBoundary(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	setZaiSelfResumeReset(t, st, &limitErr, now.Add(-5*time.Second))
	replaceZaiSelfResumeNow(t, func() time.Time { return now })

	var waitTargets []time.Time
	replaceZaiSelfResumeWait(t, func(target time.Time, _ *runner.StopController) bool {
		waitTargets = append(waitTargets, target)
		if target.After(now) {
			now = target
		}
		return false
	})

	resumes := 0
	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return limitErr },
		func() error {
			resumes++
			if resumes == 1 {
				newReset := now.Add(time.Hour)
				setZaiSelfResumeReset(t, st, &limitErr, newReset)
				return limitErr
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resumes != 2 {
		t.Fatalf("resumes = %d want 2", resumes)
	}
	if len(waitTargets) != 2 {
		t.Fatalf("wait targets = %v want post-reset cadence and new future boundary", waitTargets)
	}
	if got := waitTargets[1].Sub(waitTargets[0]); got < time.Hour {
		t.Fatalf("new future boundary wait = %s; must return to boundary-based waiting", got)
	}
}

func TestWaitForZaiFiveHourSelfResumeInterruptKeepsDurableStop(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	replaceZaiSelfResumeWait(t, func(_ time.Time, _ *runner.StopController) bool { return true })

	err := waitForZaiFiveHourSelfResume(st, controller, limitErr)
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatalf("wait error = %v, want InterruptedCallError", err)
	}
	if interrupted.TaskID != limitErr.TaskID {
		t.Fatalf("interrupted task = %q want %q", interrupted.TaskID, limitErr.TaskID)
	}
	if got := st.TaskStatus(); got != state.TaskStatusRateLimited {
		t.Fatalf("interrupted wait changed durable stop status to %s", got)
	}
	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if checkpoint.StopKind != state.ResumeStopRateLimited || checkpoint.ResetAtRFC3339 != limitErr.Limit.ResetAtRFC3339 {
		t.Fatalf("interrupted wait changed checkpoint: %#v", checkpoint)
	}
}

func TestWaitForZaiFiveHourSelfResumePastBoundaryDoesNotAddFixedDelay(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	stale := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	setZaiSelfResumeReset(t, st, &limitErr, stale)
	controller := runner.NewStopController()
	waitCalled := false
	replaceZaiSelfResumeWait(t, func(_ time.Time, _ *runner.StopController) bool {
		waitCalled = true
		return false
	})

	if err := waitForZaiFiveHourSelfResume(st, controller, limitErr); err != nil {
		t.Fatal(err)
	}
	if waitCalled {
		t.Fatal("past reset boundary must leave cadence control to the post-reset retry path")
	}
}

func TestWaitForZaiFiveHourSelfResumeFailsClosedWhenWakeBindingChanges(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	replaceZaiSelfResumeWait(t, func(_ time.Time, _ *runner.StopController) bool {
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Fatal(err)
		}
		checkpoint.ResetAtRFC3339 = time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
		if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
			t.Fatal(err)
		}
		return false
	})

	err := waitForZaiFiveHourSelfResume(st, controller, limitErr)
	if err == nil || !strings.Contains(err.Error(), "five-hour self-resume wake validation failed") || !strings.Contains(err.Error(), "reset boundary changed") {
		t.Fatalf("wake binding error = %v", err)
	}
	if got := st.TaskStatus(); got != state.TaskStatusRateLimited {
		t.Fatalf("wake validation failure changed durable stop status to %s", got)
	}
}

func TestValidateZaiFiveHourSelfResumeStateRejectsTaskIdentityChange(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	limitErr.TaskID = "different-task"
	if err := validateZaiFiveHourSelfResumeState(st, limitErr); err == nil || !strings.Contains(err.Error(), "task identity changed") {
		t.Fatalf("identity validation error = %v", err)
	}
}
