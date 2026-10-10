package parentactioncmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type parentActionExecutionKind uint8

type parentActionCommandDescriptor struct {
	Action           string
	Execute          parentActionExecutionKind
	TerminalExecute  parentActionExecutionKind
	TerminalEnvelope bool
	Payload          parentaction.PayloadAction
}

const (
	parentActionExecutionUnsupported parentActionExecutionKind = iota
	parentActionExecutionController
	parentActionExecutionPayload
	parentActionExecutionSessionRotation
	parentActionExecutionWait
	parentActionExecutionContinuationOrApprove
	parentActionExecutionDirectWorker
	parentActionExecutionStartSingle
	parentActionExecutionRead
	parentActionExecutionGitEvidence
	parentActionExecutionPreflightDecision
	parentActionExecutionObservationExecute
	parentActionExecutionExportBundle
)

var parentActionCommands = map[string]parentActionCommandDescriptor{
	"rotation-claim": {
		Action:  "rotation-claim",
		Execute: parentActionExecutionSessionRotation,
	},
	"rotation-bind": {
		Action:  "rotation-bind",
		Execute: parentActionExecutionSessionRotation,
	},
	"rotation-fail": {
		Action:  "rotation-fail",
		Execute: parentActionExecutionSessionRotation,
	},
	"wait": {
		Action:  "wait",
		Execute: parentActionExecutionWait,
	},
	actionContinuationStopHook: {
		Action:  actionContinuationStopHook,
		Execute: parentActionExecutionContinuationOrApprove,
	},
	actionContinuationMetadataGuard: {
		Action:  actionContinuationMetadataGuard,
		Execute: parentActionExecutionContinuationOrApprove,
	},
	actionApprove: {
		Action:           actionApprove,
		Execute:          parentActionExecutionContinuationOrApprove,
		TerminalEnvelope: true,
	},
	actionStart: {
		Action:           actionStart,
		Execute:          parentActionExecutionDirectWorker,
		TerminalExecute:  parentActionExecutionStartSingle,
		TerminalEnvelope: true,
	},
	actionAccept: {
		Action:           actionAccept,
		Execute:          parentActionExecutionDirectWorker,
		TerminalEnvelope: true,
	},
	actionResume: {
		Action:           actionResume,
		Execute:          parentActionExecutionDirectWorker,
		TerminalEnvelope: true,
	},
	"evidence": {
		Action:  "evidence",
		Execute: parentActionExecutionRead,
	},
	"export-bundle": {
		Action:  "export-bundle",
		Execute: parentActionExecutionExportBundle,
	},
	"finalize-check": {
		Action:  "finalize-check",
		Execute: parentActionExecutionGitEvidence,
	},
	actionObservationExecute: {
		Action:  actionObservationExecute,
		Execute: parentActionExecutionObservationExecute,
	},
}

func lookupParentActionCommand(action string) (parentActionCommandDescriptor, bool) {
	if parentaction.IsControllerAction(action) {
		return parentActionCommandDescriptor{Action: action, Execute: parentActionExecutionController, TerminalExecute: parentActionExecutionController}, true
	}
	if payload, ok := parentaction.LookupPayloadAction(action); ok {
		terminalExecute := parentActionExecutionPayload
		if payload.Action == parentaction.ActionDecision {
			terminalExecute = parentActionExecutionPreflightDecision
		}
		return parentActionCommandDescriptor{
			Action:           action,
			Execute:          parentActionExecutionPayload,
			TerminalExecute:  terminalExecute,
			TerminalEnvelope: payload.Action != parentaction.ActionReviseMilestones,
			Payload:          payload,
		}, true
	}
	descriptor, ok := parentActionCommands[action]
	if !ok {
		return parentActionCommandDescriptor{}, false
	}
	if descriptor.TerminalExecute == parentActionExecutionUnsupported {
		descriptor.TerminalExecute = descriptor.Execute
	}
	return descriptor, true
}

func executeParentActionCommand(
	cfg config.AppConfig,
	descriptor parentActionCommandDescriptor,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	terminal bool,
) error {
	execution := descriptor.Execute
	if terminal {
		execution = descriptor.TerminalExecute
	}
	if err, handled := executeStandardParentAction(cfg, descriptor, execution, args, stdout, stderr); handled {
		return err
	}
	return executeSpecialParentAction(cfg, execution, args, stdout, stderr)
}

func executeStandardParentAction(
	cfg config.AppConfig,
	descriptor parentActionCommandDescriptor,
	execution parentActionExecutionKind,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) (error, bool) {
	if execution == parentActionExecutionSessionRotation {
		return executeSessionRotationAction(cfg, args, stdout), true
	}
	return executeInterfaceParentAction(cfg, descriptor, execution, args, stdout, stderr)
}

func executeInterfaceParentAction(
	cfg config.AppConfig,
	descriptor parentActionCommandDescriptor,
	execution parentActionExecutionKind,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) (error, bool) {
	switch execution {
	case parentActionExecutionController:
		return executeControllerAction(cfg, args, stdout, stderr), true
	case parentActionExecutionPayload:
		return executeStagedPayloadAction(cfg, descriptor.Payload, args, stdout, stderr), true
	case parentActionExecutionWait:
		return executeParentWait(cfg, args, stdout, stderr), true
	case parentActionExecutionContinuationOrApprove:
		return executeContinuationOrApproveAction(cfg, descriptor.Action, args, stdout, stderr), true
	case parentActionExecutionDirectWorker:
		return executeDirectWorkerAction(cfg, descriptor.Action, args, stdout, stderr), true
	case parentActionExecutionStartSingle:
		return executeStartSingleAction(cfg, args, stdout, stderr), true
	case parentActionExecutionRead:
		return executeParentReadAction(cfg, args, stdout, stderr), true
	case parentActionExecutionExportBundle:
		return executeExportBundleAction(cfg, args, stdout, stderr), true
	case parentActionExecutionGitEvidence:
		return executeGitEvidenceAction(cfg, args, stdout), true
	case parentActionExecutionObservationExecute:
		return executeObservationExecuteAction(cfg, args, stdout), true
	default:
		return nil, false
	}
}

func executeSpecialParentAction(
	cfg config.AppConfig,
	execution parentActionExecutionKind,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if execution == parentActionExecutionPreflightDecision {
		return executePreflightedDecision(cfg, args, stdout, stderr)
	}
	return fmt.Errorf("%s", usage)
}
