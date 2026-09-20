package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type commandDispatchOwner uint8

const (
	dispatchReadOnly commandDispatchOwner = iota
	dispatchRuntimeControl
	dispatchStateCommand
	dispatchLockedMutation
	dispatchWorkflow
)

func commandDispatchOwnerFor(mode CommandMode) (commandDispatchOwner, error) {
	switch mode {
	case ModeStatus,
		ModeHandoff,
		ModeTimeline,
		ModeConvergence,
		ModeStats,
		ModeCheckWakeCoalesce,
		ModeVerifyAutoResume,
		ModeEvalAB,
		ModeCallOutliers,
		ModeCodexLimit,
		ModeModelRouting,
		ModeTestImpact,
		ModeBundle,
		ModeReviewGap,
		ModeRepoSearch,
		ModeRepoSearchEval,
		ModePacketCheck,
		ModeProjectState,
		ModeEvidence:
		return dispatchReadOnly, nil
	case ModeStop, ModeCodexWakePlan, ModeCodexWakeResponse, ModeAutoResumePlan, ModeAutoResumeResponse:
		return dispatchRuntimeControl, nil
	case ModeVerifyCodexWake, ModeInstallSmoke, ModeQualityGate:
		return dispatchStateCommand, nil
	case ModeReset,
		ModeAccept,
		ModeIsolate,
		ModePark,
		ModeUnpark,
		ModeExecutionMilestonesRevise,
		modeRotateInstructionBaseline,
		modeRecoverParentAction,
		modeRecoverQualitySurface:
		return dispatchLockedMutation, nil
	case ModeNewTask, ModeDecision, ModeFix, ModeApproveSurface, ModeResume:
		return dispatchWorkflow, nil
	default:
		return 0, fmt.Errorf("unsupported command mode: %d", mode)
	}
}

func executeRuntimeControl(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeStop:
		return requestStop(cfg, stdout)
	case ModeCodexWakePlan:
		return printCodexWakePlan(cmd, cfg, stdout)
	case ModeCodexWakeResponse:
		return printCodexWakeResponse(cmd, cfg, stdout)
	case ModeAutoResumePlan:
		return printAutoResumePlan(cmd, cfg, stdout)
	case ModeAutoResumeResponse:
		return printAutoResumeResponse(cmd, cfg, stdout)
	default:
		return fmt.Errorf("command mode %d is not runtime control", cmd.Mode)
	}
}

func executeReadOnly(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeTimeline,
		ModeConvergence,
		ModeEvalAB,
		ModeCallOutliers,
		ModeModelRouting,
		ModeTestImpact,
		ModeRepoSearchEval,
		ModeBundle,
		ModeReviewGap:
		return executeReadOnlyAnalysis(cmd, cfg, stdout)
	default:
		return executeReadOnlyInspection(cmd, cfg, stdout)
	}
}

func executeReadOnlyInspection(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	st := state.AttachStateStore(cfg)
	switch cmd.Mode {
	case ModeStatus:
		return printStatusLeased(st, stdout)
	case ModeHandoff:
		return executeHandoffInspection(cmd, cfg, st, stdout)
	case ModeStats:
		return report.PrintStats(cfg, st, cmd.Query, printTelemetryCompactSummary, stdout)
	case ModeCodexLimit:
		return printCodexLimit(cfg, stdout)
	case ModePacketCheck:
		return executeStatelessProjection(cmd, cfg, stdout)
	case ModeProjectState:
		return executeProjectStateInspection(cmd, cfg, st, stdout)
	case ModeRepoSearch, ModeEvidence, ModeCheckWakeCoalesce, ModeVerifyAutoResume:
		return executeReadOnlyProjection(cmd, cfg, st, stdout)
	default:
		return fmt.Errorf("command mode %d is not read-only inspection", cmd.Mode)
	}
}

func executeReadOnlyProjection(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeRepoSearch:
		return printRepoSearch(repoSearchRequest{
			Question:    cmd.Payload,
			Scopes:      cmd.SearchScopes,
			BudgetBytes: cmd.SearchBudgetBytes,
		}, cfg, st, stdout)
	case ModeEvidence:
		return printParentEvidence(cmd, cfg, st, stdout)
	case ModeCheckWakeCoalesce:
		return printCheckWakeCoalesce(cmd, cfg, stdout)
	case ModeVerifyAutoResume:
		return printVerifyAutoResume(cmd, cfg, stdout)
	default:
		return fmt.Errorf("command mode %d is not a read-only projection", cmd.Mode)
	}
}

func executeHandoffInspection(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	if cmd.Payload == "recovery" {
		return printParentHandoffRecoveryLeasedWithConfig(cfg, st, stdout)
	}
	return printParentHandoffLeasedWithConfig(cfg, st, stdout)
}

func executeReadOnlyAnalysis(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	st := state.AttachStateStore(cfg)
	switch cmd.Mode {
	case ModeTimeline:
		return report.PrintTimeline(st, cmd.Payload, stdout)
	case ModeConvergence:
		return report.PrintConvergence(st, cmd.Payload, stdout)
	case ModeEvalAB:
		st.EnableRepoSearchReadProjection()
		return report.PrintEvalAB(st, cmd.Payload, stdout)
	case ModeCallOutliers:
		return report.PrintCallOutliers(cfg, st, cmd.Query, printTelemetryCompactSummary, stdout)
	case ModeModelRouting:
		return report.PrintModelRouting(st, stdout)
	case ModeTestImpact:
		return report.PrintTestImpact(st, stdout)
	case ModeRepoSearchEval:
		return report.PrintRepoSearchEval(st, stdout)
	case ModeBundle:
		return printBundle(cfg, st, cmd.Payload, stdout)
	case ModeReviewGap:
		return printReviewGap(cfg, st, cmd.Payload, stdout)
	default:
		return fmt.Errorf("command mode %d is not read-only analysis", cmd.Mode)
	}
}

func executeStateCommand(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeVerifyCodexWake:
		return printVerifyCodexWake(cmd, cfg, stdout)
	case ModeInstallSmoke:
		return runInstallSmoke(cmd.Role, cfg, st, stdout)
	case ModeQualityGate:
		return runQualityGate(cmd.Payload, st, stdout)
	default:
		return fmt.Errorf("command mode %d is not a state command", cmd.Mode)
	}
}

func executeLockedMutation(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeReset:
		return executeDispositionReset(cmd, st, stdout)
	case ModeAccept:
		return parentAccept(st, stdout)
	case ModeIsolate:
		return isolateInterruptedTask(st, cfg, stdout)
	case ModePark:
		return workflow.NewWorkflow(cfg, st, nil, stdout).ExecutePark(stdout)
	case ModeUnpark:
		return workflow.NewWorkflow(cfg, st, nil, stdout).ExecuteUnpark(stdout)
	case ModeExecutionMilestonesRevise:
		return executeExecutionMilestoneRevision(cmd, cfg, st, stdout)
	case modeRotateInstructionBaseline:
		return rotateInstructionBaseline(cfg, st, stdout)
	case modeRecoverParentAction:
		return recoverInterruptedParentAction(st, stdout)
	case modeRecoverQualitySurface:
		return recoverQualitySurfaceLifecycle(st, cmd.Payload, stdout)
	default:
		return fmt.Errorf("command mode %d is not a locked mutation", cmd.Mode)
	}
}
