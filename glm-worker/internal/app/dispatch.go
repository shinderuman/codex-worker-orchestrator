package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
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
		ModeWatch,
		ModeTimeline,
		ModeConvergence,
		ModeStats,
		ModeCheckWakeCoalesce,
		ModeEvalAB,
		ModeCallOutliers,
		ModeCodexLimit,
		ModeModelRouting,
		ModeTestImpact,
		ModeBundle,
		ModeParentUsage,
		ModeReviewGap,
		ModeRepoSearch,
		ModeRepoSearchEval,
		ModePacketCheck,
		ModeProjectState,
		ModeEvidence:
		return dispatchReadOnly, nil
	case ModeStop, ModeCodexWakePlan, ModeCodexWakeResponse:
		return dispatchRuntimeControl, nil
	case ModeVerifyAutoResume, ModeVerifyCodexWake, ModeInstallSmoke, ModeQualityGate:
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
		ModeParentUsage,
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
		if cmd.Payload == "recovery" {
			return printParentHandoffRecoveryLeasedWithConfig(cfg, st, stdout)
		}
		return printParentHandoffLeasedWithConfig(cfg, st, stdout)
	case ModeStats:
		return printStats(cfg, st, cmd.Query, stdout)
	case ModeWatch:
		return printWatch(st, stdout, defaultWatchOptions(cmd.WatchVerbose))
	case ModeCodexLimit:
		return printCodexLimit(cfg, stdout)
	case ModePacketCheck, ModeProjectState:
		return executeStatelessProjection(cmd, cfg, stdout)
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
	default:
		return fmt.Errorf("command mode %d is not read-only inspection", cmd.Mode)
	}
}

func executeReadOnlyAnalysis(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	st := state.AttachStateStore(cfg)
	switch cmd.Mode {
	case ModeTimeline:
		return printTimeline(st, cmd.Payload, stdout)
	case ModeConvergence:
		return printConvergence(st, cmd.Payload, stdout)
	case ModeEvalAB:
		return printEvalAB(st, cmd.Payload, stdout)
	case ModeCallOutliers:
		return printCallOutliers(cfg, st, cmd.Query, stdout)
	case ModeModelRouting:
		return printModelRouting(st, stdout)
	case ModeTestImpact:
		return printTestImpact(st, stdout)
	case ModeRepoSearchEval:
		return printRepoSearchEval(st, stdout)
	case ModeBundle:
		return printBundle(cfg, st, cmd.Payload, stdout)
	case ModeParentUsage:
		return printParentUsage(cfg, st, cmd.Payload, stdout)
	case ModeReviewGap:
		return printReviewGap(cfg, st, cmd.Payload, stdout)
	default:
		return fmt.Errorf("command mode %d is not read-only analysis", cmd.Mode)
	}
}

func executeStateCommand(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	switch cmd.Mode {
	case ModeVerifyAutoResume:
		return printVerifyAutoResume(cmd, cfg, stdout)
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
		return resetState(st, stdout)
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
