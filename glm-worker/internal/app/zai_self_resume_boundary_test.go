package app

import (
	"errors"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteZaiSelfResumeLoopDoesNotRetryUnrelatedFailure(t *testing.T) {
	st, _ := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	unrelated := errors.New("generic provider failure")
	resumeCalls := 0

	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return unrelated },
		func() error {
			resumeCalls++
			return nil
		},
	)
	if !errors.Is(err, unrelated) {
		t.Fatalf("error = %v want unrelated failure", err)
	}
	if resumeCalls != 0 {
		t.Fatalf("resume calls = %d want 0 for unrelated failure", resumeCalls)
	}
}

func TestExecuteZaiSelfResumeLoopHonorsStopDuringPostResetRetryWait(t *testing.T) {
	st, limitErr := prepareZaiSelfResumeStop(t)
	controller := runner.NewStopController()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	setZaiSelfResumeReset(t, st, &limitErr, now.Add(-5*time.Second))
	replaceZaiSelfResumeNow(t, func() time.Time { return now })
	controller.Request()

	resumeCalls := 0
	err := executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return limitErr },
		func() error {
			resumeCalls++
			return nil
		},
	)
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatalf("error = %v want InterruptedCallError", err)
	}
	if resumeCalls != 0 {
		t.Fatalf("resume calls = %d want 0 after stop request", resumeCalls)
	}
	if got := st.TaskStatus(); got != state.TaskStatusRateLimited {
		t.Fatalf("task status = %s want %s", got, state.TaskStatusRateLimited)
	}
}
