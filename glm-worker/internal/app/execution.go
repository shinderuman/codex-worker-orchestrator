package app

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

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
		payload, err := readStdinPayload(stdin, cmd.StdinBytes, cmd.SHA256)
		if err != nil {
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
	if cmd.Mode == ModeNewTask {
		if err := requireNormalTaskHarness(cfg); err != nil {
			return err
		}
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

	recorder := newWorkflowStageRecorder(st)
	defer recorder.FlushOrArchive()
	lock, err := acquireWorkflowLockTimed(recorder, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := recorder.Measure(state.StageParentAdmission, func() error {
		return admitParentCommand(cmd, st)
	}); err != nil {
		return err
	}
	if _, err := controllerActivateTimed(recorder, cfg); err != nil {
		return err
	}

	switch owner {
	case dispatchLockedMutation:
		return executeLockedMutation(cmd, cfg, st, stdout)
	case dispatchWorkflow:
		return executeWorkflow(cmd, cfg, st, rf, stdout, recorder)
	default:
		return fmt.Errorf("unsupported state-backed dispatch owner: %d", owner)
	}
}

func newWorkflowStageRecorder(st *state.StateStore) *state.StageRecorder {
	recorder := state.NewStageRecorder(st)
	recorder.RecordParentDispatch(time.Now().UTC())
	return recorder
}

func acquireWorkflowLockTimed(recorder *state.StageRecorder, cfg config.AppConfig) (*RepoLock, error) {
	var lock *RepoLock
	err := recorder.Measure(state.StageWorkflowLock, func() error {
		acquired, lockErr := acquireWorkflowLock(cfg)
		lock = acquired
		return lockErr
	})
	return lock, err
}

func controllerActivateTimed(recorder *state.StageRecorder, cfg config.AppConfig) (controller.Admission, error) {
	span := recorder.Span(state.StageControllerActivate)
	admission, err := controller.Activate(cfg)
	span.End(err)
	return admission, err
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
	recorder := newWorkflowStageRecorder(attached)
	defer recorder.FlushOrArchive()
	lock, err := acquireWorkflowLockTimed(recorder, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	if err := recorder.Measure(state.StageParentAdmission, func() error {
		return admitParentCommand(cmd, attached)
	}); err != nil {
		return err
	}
	if cmd.Mode == ModeAccept && attached.TaskStatus() == state.TaskStatusNone {
		return parentAccept(attached, stdout)
	}
	cfg, err = activateWorkflowConfigTimed(recorder, cmd, cfg)
	if err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	recorder.RebindStore(st)
	if cmd.Mode == ModeAccept {
		return acceptRetainedCanonicalWorkflow(cfg, st, stdout)
	}
	return executeWorkflow(cmd, cfg, st, rf, stdout, recorder)
}

func acceptRetainedCanonicalWorkflow(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	binding, err := st.LoadControllerRuntimeBinding()
	if err != nil || head.ExecutionTaskRef == nil || binding.AttemptID != head.LiveAttemptID || binding.TaskPath != head.ExecutionTaskRef.TaskPath || binding.RequestID != head.ExecutionTaskRef.RequestID || binding.TaskContractDigest != head.ExecutionTaskRef.ContractDigest {
		return fmt.Errorf("parent acceptance requires review reentry for the current controller attempt")
	}
	return parentAccept(st, stdout)
}

func requireNormalTaskHarness(cfg config.AppConfig) error {
	present, err := controller.RepositoryPresent(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("inspect normal task repository: %w", err)
	}
	if !present {
		return fmt.Errorf("normal task admission requires a Git repository; canonical non-Git task authority is not defined")
	}
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("evaluate normal task repository harness: %w", err)
	}
	if decision.Active {
		return nil
	}
	if decision.Reason == repositoryharness.ReasonAbsent {
		tracked, err := repositoryharness.MarkerTracked(cfg.RepoRoot)
		if err != nil {
			return fmt.Errorf("inspect missing harness marker tracking: %w", err)
		}
		if tracked {
			return fmt.Errorf("normal task admission refuses missing tracked repository harness marker (%s: %s)", repositoryharness.MarkerPath, decision.Reason)
		}
		return nil
	}
	return fmt.Errorf("normal task admission refuses invalid repository harness marker (%s: %s)", repositoryharness.MarkerPath, decision.Reason)
}

func requireWorkflowAdmission(cmd Command, cfg config.AppConfig) error {
	if cmd.Mode == ModeNewTask {
		return requireNormalTaskHarness(cfg)
	}
	return requireExistingWorkflowController(cfg)
}

func activateWorkflowConfig(cmd Command, cfg config.AppConfig) (config.AppConfig, error) {
	if err := requireWorkflowAdmission(cmd, cfg); err != nil {
		return cfg, err
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return cfg, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return cfg, err
	}
	if err := rejectLiveParentOnlyExecution(store); err != nil {
		return cfg, err
	}
	if cmd.Mode == ModeNewTask {
		return activateNewWorkflowConfig(cfg, store, head, cmd.Payload)
	}
	_, err = controller.Activate(cfg)
	return cfg, err
}

func activateNewWorkflowConfig(cfg config.AppConfig, store *controller.Store, head controller.RepositoryControllerHead, request string) (config.AppConfig, error) {
	requestScope, err := workflowRequestScope(store, head)
	if err != nil {
		return cfg, err
	}
	if !requestScope && idleProjectWorkflowHead(head) {
		return activateIdleWorkflowConfig(cfg, store, request)
	}
	if !requestScope && !controller.IsPristine(head) && head.LiveLeaseID == "" && head.ExecutionTaskRef == nil {
		materialized, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration})
		if err != nil {
			return cfg, err
		}
		cfg.RepoRoot = materialized.Admission.Workspace.Root
		return controller.WorkflowConfig(cfg)
	}
	if head.LiveLeaseID != "" {
		bound, err := controller.ParentContinuationConfig(cfg)
		if err != nil {
			return cfg, err
		}
		cfg = bound
	}
	if _, err := controller.ActivateRequest(cfg, request); err != nil {
		return cfg, err
	}
	return controller.WorkflowConfig(cfg)
}

func workflowRequestScope(store *controller.Store, head controller.RepositoryControllerHead) (bool, error) {
	if head.ProjectSnapshotID == "" {
		return false, nil
	}
	project, err := store.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return false, err
	}
	return project.UserRequest != nil || project.CompletedRequest != nil, nil
}

func idleProjectWorkflowHead(head controller.RepositoryControllerHead) bool {
	return head.RootTaskRef == nil && head.ExecutionTaskRef == nil && head.LiveLeaseID == ""
}

func activateIdleWorkflowConfig(cfg config.AppConfig, store *controller.Store, request string) (config.AppConfig, error) {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return cfg, err
	}
	if decision.Active {
		authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
		if err != nil {
			return cfg, err
		}
		if authority.Task.Empty() {
			if _, err := store.StartRegisteredTask(); err != nil {
				return cfg, err
			}
			return controller.WorkflowConfig(cfg)
		}
	}
	if _, err := controller.ActivateRequest(cfg, request); err != nil {
		return cfg, err
	}
	return controller.WorkflowConfig(cfg)
}

func requireExistingWorkflowController(cfg config.AppConfig) error {
	exists, err := controller.Exists(cfg)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("existing workflow requires canonical controller execution authority")
	}
	return nil
}

func rejectLiveParentOnlyExecution(store *controller.Store) error {
	live, err := store.LiveParentOnlyExecution()
	if err != nil {
		return err
	}
	if live {
		return fmt.Errorf("parent-only execution completes through the parent completion path; Runtime start is rejected")
	}
	return nil
}

func activateWorkflowConfigTimed(recorder *state.StageRecorder, cmd Command, cfg config.AppConfig) (config.AppConfig, error) {
	span := recorder.Span(state.StageControllerActivate)
	bound, err := activateWorkflowConfig(cmd, cfg)
	span.End(err)
	return bound, err
}

func executeWorkflow(cmd Command, cfg config.AppConfig, st *state.StateStore, rf RunnerFactory, stdout io.Writer, recorder *state.StageRecorder) error {
	if err := recorder.Measure(state.StageQualityPreflight, func() error {
		return preflightQualityToolchain(cfg, st)
	}); err != nil {
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
	wf.AttachStageRecorder(recorder)
	if cmd.Mode != ModeNewTask {
		recorder.BindCurrentTask()
	}

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
	case ModeResubmitReport:
		return wf.ExecuteReportResubmission()
	default:
		return fmt.Errorf("unsupported command mode")
	}
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
