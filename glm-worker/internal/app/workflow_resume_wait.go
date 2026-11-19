package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const (
	zaiPostResetRetryInterval       = 10 * time.Second
	zaiPostResetRetryWindow         = 2 * time.Minute
	zaiSelfResumeModeInitialGrace   = "initial-grace"
	zaiSelfResumeModePostResetRetry = "post-reset-retry"
)

func zaiSelfResumeNow() time.Time { return time.Now().UTC() }

func waitForZaiSelfResume(target time.Time, controller *runner.StopController) bool {
	if controller.StopRequested() {
		return true
	}
	delay := time.Until(target)
	if delay <= 0 {
		return controller.StopRequested()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return controller.StopRequested()
	case <-controller.Requested():
		return true
	}
}

func executeWorkflowWithZaiSelfResume(
	cmd Command,
	wf *workflow.Workflow,
	st *state.StateStore,
	controller *runner.StopController,
) error {
	return executeZaiSelfResumeLoop(
		st,
		controller,
		func() error { return executeWorkflowCommand(cmd, wf) },
		func() error { return wf.ExecuteResumeWithExecutionMilestones() },
		zaiSelfResumeNow,
		waitForZaiSelfResume,
	)
}

func executeZaiSelfResumeLoop(
	st *state.StateStore,
	controller *runner.StopController,
	initial func() error,
	resume func() error,
	now func() time.Time,
	wait func(time.Time, *runner.StopController) bool,
) error {
	lastAttemptStartedAt := now()
	err := initial()
	lastBoundary := ""
	for {
		var scheduled *runner.ScheduledPauseError
		if errors.As(err, &scheduled) {
			if waitErr := waitForScheduledPause(st, controller, scheduled, now, wait); waitErr != nil {
				return waitErr
			}
			lastAttemptStartedAt = now()
			err = resume()
			continue
		}
		var limitErr runner.ZaiRateLimitError
		if !errors.As(err, &limitErr) {
			return err
		}
		postReset, boundary, retryErr := classifyZaiSelfResumeRetry(lastBoundary, limitErr, now())
		if retryErr != nil {
			return retryErr
		}
		if waitErr := waitForZaiFiveHourSelfResume(st, controller, limitErr, now, wait); waitErr != nil {
			return waitErr
		}
		if waitErr := waitForZaiPostResetRetryIfNeeded(st, controller, limitErr, postReset, lastAttemptStartedAt, now, wait); waitErr != nil {
			return waitErr
		}
		lastBoundary = boundary
		lastAttemptStartedAt = now()
		err = resume()
	}
}

func classifyZaiSelfResumeRetry(lastBoundary string, limitErr runner.ZaiRateLimitError, now time.Time) (bool, string, error) {
	boundary := limitErr.Limit.ResetAtRFC3339
	resumeAt, err := zaiSelfResumeAt(boundary)
	if err != nil {
		return false, boundary, errors.Join(limitErr, err)
	}
	if resumeAt.After(now) {
		return false, boundary, nil
	}
	if lastBoundary != "" && lastBoundary != boundary {
		return true, boundary, errors.Join(limitErr, fmt.Errorf(
			"five-hour self-resume received a different already-passed reset boundary: got %s want %s",
			boundary,
			lastBoundary,
		))
	}
	if !now.Before(resumeAt.Add(zaiPostResetRetryWindow)) {
		return true, boundary, errors.Join(limitErr, fmt.Errorf(
			"five-hour self-resume post-reset retry window exhausted for reset boundary %s",
			boundary,
		))
	}
	return true, boundary, nil
}

func zaiSelfResumeAt(resetAtRFC3339 string) (time.Time, error) {
	available, resumeAtRFC3339 := runner.AutoResumeAtFromReset(resetAtRFC3339)
	if !available {
		return time.Time{}, fmt.Errorf("five-hour self-resume reset boundary is unavailable")
	}
	resumeAt, err := time.Parse(time.RFC3339, resumeAtRFC3339)
	if err != nil {
		return time.Time{}, fmt.Errorf("five-hour self-resume boundary is invalid: %w", err)
	}
	return resumeAt, nil
}

func zaiSelfResumeMode(resetAtRFC3339 string, now time.Time) string {
	resumeAt, err := zaiSelfResumeAt(resetAtRFC3339)
	if err != nil {
		return ""
	}
	if resumeAt.After(now) {
		return zaiSelfResumeModeInitialGrace
	}
	return zaiSelfResumeModePostResetRetry
}

func waitForZaiFiveHourSelfResume(
	st *state.StateStore,
	controller *runner.StopController,
	limitErr runner.ZaiRateLimitError,
	now func() time.Time,
	wait func(time.Time, *runner.StopController) bool,
) error {
	if err := validateZaiFiveHourSelfResumeState(st, limitErr); err != nil {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume pre-wait validation failed: %w", err))
	}
	resumeAt, err := zaiSelfResumeAt(limitErr.Limit.ResetAtRFC3339)
	if err != nil {
		return errors.Join(limitErr, err)
	}
	if resumeAt.After(now()) && wait(resumeAt, controller) {
		return zaiSelfResumeInterrupted(controller, limitErr)
	}
	if err := validateZaiFiveHourSelfResumeState(st, limitErr); err != nil {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume wake validation failed: %w", err))
	}
	return nil
}

func waitForZaiPostResetRetryIfNeeded(
	st *state.StateStore,
	controller *runner.StopController,
	limitErr runner.ZaiRateLimitError,
	postReset bool,
	lastAttemptStartedAt time.Time,
	now func() time.Time,
	wait func(time.Time, *runner.StopController) bool,
) error {
	if !postReset {
		return nil
	}
	return waitForZaiPostResetRetrySpacing(st, controller, limitErr, lastAttemptStartedAt, now, wait)
}

func waitForZaiPostResetRetrySpacing(
	st *state.StateStore,
	controller *runner.StopController,
	limitErr runner.ZaiRateLimitError,
	lastAttemptStartedAt time.Time,
	now func() time.Time,
	wait func(time.Time, *runner.StopController) bool,
) error {
	if lastAttemptStartedAt.IsZero() {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume previous attempt start time is unavailable"))
	}
	nextAttemptAt := lastAttemptStartedAt.Add(zaiPostResetRetryInterval)
	if nextAttemptAt.After(now()) && wait(nextAttemptAt, controller) {
		return zaiSelfResumeInterrupted(controller, limitErr)
	}
	if err := validateZaiFiveHourSelfResumeState(st, limitErr); err != nil {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume post-reset retry validation failed: %w", err))
	}
	return nil
}

func zaiSelfResumeInterrupted(controller *runner.StopController, limitErr runner.ZaiRateLimitError) error {
	controller.NotifyInterrupted(limitErr.TaskID, "")
	return &runner.InterruptedCallError{
		Phase:    limitErr.Phase,
		TaskID:   limitErr.TaskID,
		RepoRoot: limitErr.RepoRoot,
	}
}

func validateZaiFiveHourSelfResumeState(st *state.StateStore, limitErr runner.ZaiRateLimitError) error {
	if st.TaskStatus() != state.TaskStatusRateLimited {
		return fmt.Errorf("task is no longer rate-limited: %s", st.TaskStatus())
	}
	taskID, err := st.TaskID()
	if err != nil {
		return fmt.Errorf("task identity is unavailable: %w", err)
	}
	if limitErr.TaskID == "" || taskID != limitErr.TaskID {
		return fmt.Errorf("task identity changed: got %q want %q", taskID, limitErr.TaskID)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return fmt.Errorf("resume checkpoint is unavailable: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopRateLimited {
		return fmt.Errorf("resume checkpoint stop kind changed: %q", checkpoint.StopKind)
	}
	if limitErr.Limit.ResetAtRFC3339 == "" || checkpoint.ResetAtRFC3339 != limitErr.Limit.ResetAtRFC3339 {
		return fmt.Errorf("reset boundary changed: got %q want %q", checkpoint.ResetAtRFC3339, limitErr.Limit.ResetAtRFC3339)
	}
	return nil
}

func waitForScheduledPause(
	st *state.StateStore,
	controller *runner.StopController,
	paused *runner.ScheduledPauseError,
	now func() time.Time,
	wait func(time.Time, *runner.StopController) bool,
) error {
	validate := func() error {
		if st.TaskStatus() != state.TaskStatusScheduledPause {
			return fmt.Errorf("scheduled-pause task status changed: %s", st.TaskStatus())
		}
		taskID, err := st.TaskID()
		if err != nil {
			return fmt.Errorf("scheduled-pause task identity is unavailable: %w", err)
		}
		if taskID != paused.TaskID {
			return fmt.Errorf("scheduled-pause task identity changed: got %q want %q", taskID, paused.TaskID)
		}
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			return fmt.Errorf("scheduled-pause checkpoint is unavailable: %w", err)
		}
		if checkpoint.StopKind != state.ResumeStopScheduledPause ||
			checkpoint.ScheduledResumeAt != paused.ResumeAt.Format(time.RFC3339) {
			return fmt.Errorf("scheduled-pause checkpoint or resume boundary changed")
		}
		return nil
	}
	if err := validate(); err != nil {
		return errors.Join(paused, err)
	}
	if paused.ResumeAt.After(now()) && wait(paused.ResumeAt, controller) {
		controller.NotifyInterrupted(paused.TaskID, "")
		return &runner.InterruptedCallError{Phase: paused.Phase, TaskID: paused.TaskID, RepoRoot: paused.RepoRoot}
	}
	if err := validate(); err != nil {
		return errors.Join(paused, err)
	}
	return nil
}
