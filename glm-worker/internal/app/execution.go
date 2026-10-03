package app

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type stdinReadyControlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

const (
	zaiPostResetRetryInterval       = 10 * time.Second
	zaiPostResetRetryWindow         = 2 * time.Minute
	zaiSelfResumeModeInitialGrace   = "initial-grace"
	zaiSelfResumeModePostResetRetry = "post-reset-retry"
)

var zaiSelfResumeNow = func() time.Time { return time.Now().UTC() }

var waitForZaiSelfResume = func(target time.Time, controller *runner.StopController) bool {
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

func emitStdinReadyControlEvent(w io.Writer) error {
	line, err := taskview.MarshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		return fmt.Errorf("stdin ready control event encode failed: %w", err)
	}
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("stdin ready control event write failed: %w", err)
	}
	return nil
}

func run(
	args []string,
	loadConfig func() (config.AppConfig, error),
	runnerFactory RunnerFactory,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	cmd, err := ParseCommand(args)
	if err != nil {
		return err
	}
	if cmd.StdinBytes > 0 {
		restore, rawApplied, err := enterStdinRawMode(stdin)
		if err != nil {
			return err
		}
		if rawApplied {
			if markerErr := emitStdinReadyControlEvent(stderr); markerErr != nil {
				return errors.Join(markerErr, restore())
			}
		}
		payload, readErr := readStdinPayload(stdin, cmd.StdinBytes, cmd.SHA256)
		if err := errors.Join(readErr, restore()); err != nil {
			return err
		}
		cmd.Payload = payload
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return dispatchMachineOutput(cmd, cfg, runnerFactory, stdout, stderr)
}

func Execute(cmd Command, cfg config.AppConfig, rf RunnerFactory, stdout, _ io.Writer) error {
	if cmd.StdinBytes > 0 && cmd.Payload == "" {
		return fmt.Errorf("stdin payload mode requires the payload to be read before execute")
	}
	var err error
	cfg, err = controller.WorkflowConfig(cfg)
	if err != nil {
		return err
	}

	owner, err := commandDispatchOwnerFor(cmd.Mode)
	if err != nil {
		return err
	}
	switch owner {
	case dispatchReadOnly:
		return executeReadOnly(cmd, cfg, stdout)
	case dispatchRuntimeControl:
		return executeRuntimeControl(cmd, cfg, stdout)
	default:
		return executeStateBacked(cmd, owner, cfg, rf, stdout)
	}
}

func executeStateBacked(
	cmd Command,
	owner commandDispatchOwner,
	cfg config.AppConfig,
	rf RunnerFactory,
	stdout io.Writer,
) error {
	if err := rejectLegacyStateLifecycle(cfg, cmd.Mode); err != nil {
		return err
	}
	if retainedCanonicalWorkflowMode(cmd.Mode) {
		return executeRetainedCanonicalWorkflow(cmd, cfg, rf, stdout)
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if owner == dispatchStateCommand {
		return executeStateCommand(cmd, cfg, st, stdout)
	}

	lock, err := acquireWorkflowLock(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := admitParentCommand(cmd, st); err != nil {
		return err
	}
	if _, err := controller.Activate(cfg); err != nil {
		return err
	}

	switch owner {
	case dispatchLockedMutation:
		return executeLockedMutation(cmd, cfg, st, stdout)
	case dispatchWorkflow:
		return executeWorkflow(cmd, cfg, st, rf, stdout)
	default:
		return fmt.Errorf("unsupported state-backed dispatch owner: %d", owner)
	}
}

func retainedCanonicalWorkflowMode(mode CommandMode) bool {
	return mode == ModeNewTask || mode == ModeResume || mode == ModeDecision || mode == ModeFix || mode == ModeApproveSurface || mode == ModeAccept
}

func executeRetainedCanonicalWorkflow(
	cmd Command,
	cfg config.AppConfig,
	rf RunnerFactory,
	stdout io.Writer,
) error {
	attached := state.AttachStateStore(cfg)
	lock, err := acquireWorkflowLock(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	if err := admitParentCommand(cmd, attached); err != nil {
		return err
	}
	if cmd.Mode == ModeAccept && attached.TaskStatus() == state.TaskStatusNone {
		return parentAccept(attached, stdout)
	}
	cfg, err = activateWorkflowConfig(cmd, cfg)
	if err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if cmd.Mode == ModeAccept {
		store, err := controller.Open(cfg)
		if err != nil {
			return err
		}
		head, err := store.LoadHead()
		if err != nil {
			return err
		}
		boundAttempt := st.ReadOr(state.ControllerAttemptStateFile, "")
		if boundAttempt == "" || boundAttempt != head.LiveAttemptID {
			return fmt.Errorf("parent acceptance requires review reentry for the current controller attempt")
		}
		return parentAccept(st, stdout)
	}
	return executeWorkflow(cmd, cfg, st, rf, stdout)
}

func activateWorkflowConfig(cmd Command, cfg config.AppConfig) (config.AppConfig, error) {
	if cmd.Mode != ModeNewTask {
		exists, err := controller.Exists(cfg)
		if err != nil {
			return cfg, err
		}
		if !exists {
			return cfg, fmt.Errorf("existing workflow requires canonical controller execution authority")
		}
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return cfg, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return cfg, err
	}
	if cmd.Mode == ModeNewTask && !controller.IsPristine(head) && head.LiveLeaseID == "" && head.ExecutionTaskRef == nil {
		materialized, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration})
		if err != nil {
			return cfg, err
		}
		cfg.RepoRoot = materialized.Admission.Workspace.Root
		return controller.WorkflowConfig(cfg)
	}
	_, err = controller.Activate(cfg)
	return cfg, err
}

func executeWorkflow(cmd Command, cfg config.AppConfig, st *state.StateStore, rf RunnerFactory, stdout io.Writer) error {
	if err := preflightQualityToolchain(cfg, st); err != nil {
		return err
	}
	controller := runner.NewStopController()
	stopServer, err := startStopEndpoint(st, controller)
	if err != nil {
		return err
	}
	defer stopServer.Close()

	r := rf(cfg, st, controller)
	wf := workflow.NewWorkflow(cfg, st, r, stdout)
	wf.AttachStopController(controller)

	return executeWorkflowWithZaiSelfResume(cmd, wf, st, controller)
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
	)
}

func executeZaiSelfResumeLoop(
	st *state.StateStore,
	controller *runner.StopController,
	initial func() error,
	resume func() error,
) error {
	lastAttemptStartedAt := zaiSelfResumeNow()
	err := initial()
	lastBoundary := ""
	for {
		var limitErr runner.ZaiRateLimitError
		if !errors.As(err, &limitErr) {
			return err
		}
		postReset, boundary, retryErr := classifyZaiSelfResumeRetry(lastBoundary, limitErr)
		if retryErr != nil {
			return retryErr
		}
		if waitErr := waitForZaiFiveHourSelfResume(st, controller, limitErr); waitErr != nil {
			return waitErr
		}
		if waitErr := waitForZaiPostResetRetryIfNeeded(st, controller, limitErr, postReset, lastAttemptStartedAt); waitErr != nil {
			return waitErr
		}
		lastBoundary = boundary
		lastAttemptStartedAt = zaiSelfResumeNow()
		err = resume()
	}
}

func classifyZaiSelfResumeRetry(lastBoundary string, limitErr runner.ZaiRateLimitError) (bool, string, error) {
	boundary := limitErr.Limit.ResetAtRFC3339
	resumeAt, err := zaiSelfResumeAt(boundary)
	if err != nil {
		return false, boundary, errors.Join(limitErr, err)
	}
	now := zaiSelfResumeNow()
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

func executeWorkflowCommand(cmd Command, wf *workflow.Workflow) error {
	switch cmd.Mode {
	case ModeNewTask:
		return executeNewTaskCommand(wf, cmd)
	case ModeDecision:
		return wf.ExecuteDecisionWithExecutionUnitPayload(cmd.Payload)
	case ModeFix:
		return wf.ExecuteExplicitFixWithExecutionMilestones(cmd.Payload, cmd.Origin, cmd.Cause, cmd.AcceptedScope)
	case ModeApproveSurface:
		return wf.ExecuteQualitySurfaceApprovalWithExecutionMilestones(cmd.AcceptedScope)
	case ModeResume:
		return wf.ExecuteResumeWithExecutionMilestones()
	default:
		return fmt.Errorf("unsupported command mode")
	}
}

func waitForZaiFiveHourSelfResume(
	st *state.StateStore,
	controller *runner.StopController,
	limitErr runner.ZaiRateLimitError,
) error {
	if err := validateZaiFiveHourSelfResumeState(st, limitErr); err != nil {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume pre-wait validation failed: %w", err))
	}
	resumeAt, err := zaiSelfResumeAt(limitErr.Limit.ResetAtRFC3339)
	if err != nil {
		return errors.Join(limitErr, err)
	}
	if resumeAt.After(zaiSelfResumeNow()) && waitForZaiSelfResume(resumeAt, controller) {
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
) error {
	if !postReset {
		return nil
	}
	return waitForZaiPostResetRetrySpacing(st, controller, limitErr, lastAttemptStartedAt)
}

func waitForZaiPostResetRetrySpacing(
	st *state.StateStore,
	controller *runner.StopController,
	limitErr runner.ZaiRateLimitError,
	lastAttemptStartedAt time.Time,
) error {
	if lastAttemptStartedAt.IsZero() {
		return errors.Join(limitErr, fmt.Errorf("five-hour self-resume previous attempt start time is unavailable"))
	}
	nextAttemptAt := lastAttemptStartedAt.Add(zaiPostResetRetryInterval)
	if nextAttemptAt.After(zaiSelfResumeNow()) && waitForZaiSelfResume(nextAttemptAt, controller) {
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
