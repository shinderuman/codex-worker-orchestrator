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

func replaceZaiSelfResumeWait(t *testing.T, fn func(time.Time, *runner.StopController) bool) {
	t.Helper()
	previous := waitForZaiSelfResume
	waitForZaiSelfResume = fn
	t.Cleanup(func() { waitForZaiSelfResume = previous })
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
