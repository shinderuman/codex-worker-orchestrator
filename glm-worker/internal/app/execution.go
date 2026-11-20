package app

import (
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

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
	if cmd.ParentStageToken != "" {
		action := parentaction.ActionDecision
		if cmd.Mode == ModeFix {
			action = parentaction.ActionFix
		}
		payload, err := parentaction.ConsumeSHA256(cfg.RepoRoot, string(action), cmd.ParentStageToken, cmd.SHA256)
		if err != nil {
			return err
		}
		cmd.Payload = string(payload)
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
