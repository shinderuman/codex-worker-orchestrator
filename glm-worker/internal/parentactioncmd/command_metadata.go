package parentactioncmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type parentActionExecutionKind uint8

type parentActionHandoffKind uint8

type parentActionCommandDescriptor struct {
	Action           string
	Execute          parentActionExecutionKind
	TerminalExecute  parentActionExecutionKind
	TerminalEnvelope bool
	Handoff          parentActionHandoffKind
	Payload          parentaction.PayloadAction
}

const (
	parentActionExecutionUnsupported parentActionExecutionKind = iota
	parentActionExecutionPayload
	parentActionExecutionSessionRotation
	parentActionExecutionLifecycle
	parentActionExecutionComplete
	parentActionExecutionInstall
	parentActionExecutionWait
	parentActionExecutionContinuationOrApprove
	parentActionExecutionDirectWorker
	parentActionExecutionReadOrPark
	parentActionExecutionGitEvidence
	parentActionExecutionReviewEvidence
	parentActionExecutionPreflightDecision
	parentActionExecutionDefectRegistration
	parentActionExecutionImprovementDisposition
)

const (
	parentActionHandoffWorker parentActionHandoffKind = iota
	parentActionHandoffInProcess
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
	"no-go": {
		Action:           "no-go",
		Execute:          parentActionExecutionLifecycle,
		TerminalEnvelope: true,
	},
	actionRecordPublicationFinding: {
		Action:           actionRecordPublicationFinding,
		Execute:          parentActionExecutionLifecycle,
		TerminalEnvelope: true,
	},
	actionReopen: {
		Action:           actionReopen,
		Execute:          parentActionExecutionLifecycle,
		TerminalEnvelope: true,
	},
	"complete": {
		Action:  "complete",
		Execute: parentActionExecutionComplete,
	},
	"install": {
		Action:  "install",
		Execute: parentActionExecutionInstall,
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
	actionPark: {
		Action:           actionPark,
		Execute:          parentActionExecutionReadOrPark,
		TerminalEnvelope: true,
	},
	actionUnpark: {
		Action:           actionUnpark,
		Execute:          parentActionExecutionReadOrPark,
		TerminalEnvelope: true,
	},
	"evidence": {
		Action:  "evidence",
		Execute: parentActionExecutionReadOrPark,
	},
	"finalize-check": {
		Action:  "finalize-check",
		Execute: parentActionExecutionGitEvidence,
	},
	"push-binding": {
		Action:  "push-binding",
		Execute: parentActionExecutionGitEvidence,
	},
	actionReviewEvidence: {
		Action:           actionReviewEvidence,
		TerminalExecute:  parentActionExecutionReviewEvidence,
		TerminalEnvelope: true,
		Handoff:          parentActionHandoffInProcess,
	},
	actionRecordDefectFinding: {
		Action:           actionRecordDefectFinding,
		TerminalExecute:  parentActionExecutionDefectRegistration,
		TerminalEnvelope: true,
	},
	actionBindDefectTask: {
		Action:           actionBindDefectTask,
		TerminalExecute:  parentActionExecutionDefectRegistration,
		TerminalEnvelope: true,
	},
	actionImprovementDisposition: {
		Action:           actionImprovementDisposition,
		TerminalExecute:  parentActionExecutionImprovementDisposition,
		TerminalEnvelope: true,
	},
}

func lookupParentActionCommand(action string) (parentActionCommandDescriptor, bool) {
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
	switch execution {
	case parentActionExecutionPayload:
		return executeStagedPayloadAction(cfg, descriptor.Payload, args, stdout, stderr), true
	case parentActionExecutionSessionRotation:
		return executeSessionRotationAction(cfg, args, stdout), true
	case parentActionExecutionLifecycle:
		return executeParentLifecycleAction(cfg, args, stdout), true
	case parentActionExecutionComplete:
		return executeComplete(cfg, args, stdout), true
	case parentActionExecutionInstall:
		return executeInstall(cfg, args, stdout, stderr), true
	case parentActionExecutionWait:
		return executeParentWait(cfg, args, stdout, stderr), true
	case parentActionExecutionContinuationOrApprove:
		return executeContinuationOrApproveAction(cfg, descriptor.Action, args, stdout, stderr), true
	case parentActionExecutionDirectWorker:
		return executeDirectWorkerAction(cfg, descriptor.Action, args, stdout, stderr), true
	case parentActionExecutionReadOrPark:
		return executeParentReadOrParkAction(cfg, args, stdout, stderr), true
	case parentActionExecutionGitEvidence:
		return executeGitEvidenceAction(cfg, args, stdout), true
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
	switch execution {
	case parentActionExecutionReviewEvidence:
		return executeParentReviewEvidence(cfg, args, stdout)
	case parentActionExecutionPreflightDecision:
		return executePreflightedDecision(cfg, args, stdout, stderr)
	case parentActionExecutionDefectRegistration:
		return executeDefectRegistrationAction(cfg, args, stdout)
	case parentActionExecutionImprovementDisposition:
		return executeImprovementDisposition(cfg, args, stdout)
	default:
		return fmt.Errorf("%s", usage)
	}
}

func parentActionUsesInProcessHandoff(action string) bool {
	descriptor, ok := lookupParentActionCommand(action)
	return ok && descriptor.Handoff == parentActionHandoffInProcess
}
