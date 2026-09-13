package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type stdinReadyControlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

func emitStdinReadyControlEvent(w io.Writer) error {
	line, err := marshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		return fmt.Errorf("stdin ready control event encode failed: %w", err)
	}
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("stdin ready control event write failed: %w", err)
	}
	return nil
}

func defaultRunnerFactory(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner {
	r := runner.NewClaudeRunner(cfg, st)
	r.AttachStopController(stop)
	return r
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
	if err := bindCurrentCodexThreadIdentity(&cmd); err != nil {
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

	owner, err := commandDispatchOwnerFor(cmd.Mode)
	if err != nil {
		return err
	}
	switch owner {
	case dispatchReadOnly:
		return executeReadOnly(cmd, cfg, stdout)
	case dispatchRuntimeControl:
		return executeRuntimeControl(cmd, cfg, stdout)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if owner == dispatchStateCommand {
		return executeStateCommand(cmd, cfg, st, stdout)
	}

	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := admitParentCommand(cmd, st); err != nil {
		return err
	}

	switch owner {
	case dispatchLockedMutation:
		return executeLockedMutation(cmd, cfg, st, stdout)
	case dispatchWorkflow:
		return executeWorkflow(cmd, cfg, st, rf, stdout)
	default:
		return fmt.Errorf("unsupported dispatch owner: %d", owner)
	}
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

	switch cmd.Mode {
	case ModeNewTask:
		return executeNewTaskCommand(wf, cmd)
	case ModeDecision:
		return wf.ExecuteDecisionWithExecutionMilestones(cmd.Payload)
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
