package app

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// EvaluateCanonicalSessionRotationTerminal はTaskStatsを入力にせずcanonical stateからsession rotationを評価する。
func EvaluateCanonicalSessionRotationTerminal(
	cfg config.AppConfig,
	st *state.StateStore,
	terminal string,
	acceptedRisk string,
) (*state.SessionRotationEvaluation, error) {
	taskID, err := st.TaskID()
	if err != nil {
		return nil, err
	}
	identity, err := st.CurrentParentCodexIdentity()
	if err != nil {
		return nil, fmt.Errorf("session rotation requires a bound parent Codex thread identity: %w", err)
	}
	if identity.TaskID != taskID {
		return nil, fmt.Errorf("session rotation parent Codex identity does not match current task")
	}
	acceptedTasks, acceptedTasksAvailable, acceptedTasksSource, err := st.SessionRotationAcceptedTaskCount(identity.ThreadID)
	if err != nil {
		return nil, err
	}
	if terminal == state.SessionRotationTerminalAccept && acceptedTasksAvailable {
		acceptedTasks++
	}
	signals := state.SessionRotationSignals{
		Terminal:                 terminal,
		AcceptedTasks:            acceptedTasks,
		AcceptedTasksUnavailable: !acceptedTasksAvailable,
		AcceptedTasksSource:      acceptedTasksSource,
		CurrentAcceptedRisk:      acceptedRisk,
	}
	sessionRotationCanonicalRolloutSignals(cfg, identity.ThreadID, &signals)
	sessionRotationMaterialEventSignals(st, taskID, &signals)
	baselineUpdate, err := sessionRotationLimitSignals(cfg, st, identity.ThreadID, &signals)
	if err != nil {
		return nil, err
	}
	decision := state.DecideSessionRotation(signals)
	sessionRotationAttachMaterialEventSources(st, taskID, signals, &decision)
	return &state.SessionRotationEvaluation{
		ParentThreadID:           identity.ThreadID,
		TaskID:                   taskID,
		Terminal:                 terminal,
		Decision:                 decision,
		AcceptedTasks:            acceptedTasks,
		AcceptedTasksUnavailable: !acceptedTasksAvailable,
		LimitBaselineUpdate:      baselineUpdate,
	}, nil
}

func sessionRotationCanonicalRolloutSignals(cfg config.AppConfig, threadID string, signals *state.SessionRotationSignals) {
	association := sessionRotationCanonicalAssociation(cfg.CodexConfigDir, threadID)
	if association.ParentStatus != codexStatusIncluded {
		signals.RolloutUnavailableField = state.SessionRotationEvidenceFieldRolloutAssociation
		signals.RolloutUnavailableSrc = association.Detail
		return
	}
	chain := association.rolloutChain()
	start := sessionRotationChainStart(chain)
	end := time.Now().UTC()
	scan, err := scanCodexRolloutChainWindow(chain, start, end)
	if err != nil {
		signals.RolloutUnavailableField = state.SessionRotationEvidenceFieldRolloutScan
		signals.RolloutUnavailableSrc = association.parentSourceLabel()
		return
	}
	turns, _, _, compactions, outputBytes := parentUsageRolloutActivity(scan, start, end, false)
	signals.Rollout = &state.SessionRotationRolloutSignals{
		ModelTurns:      turns,
		ToolOutputBytes: outputBytes,
		Compactions:     compactions,
		Source:          association.parentSourceLabel(),
	}
}

func sessionRotationCanonicalAssociation(codexHome, threadID string) codexAssociation {
	if !state.ValidUUIDFormat(threadID) {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Detail: "parent Codex thread identity is invalid"}
	}
	if !codexDirExists(codexHome) {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: codexAssociationBasis, Detail: "codex home is not present"}
	}
	rollouts, err := scanCodexRollouts(codexHome)
	if err != nil {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: codexAssociationBasis, Detail: "codex rollout enumeration failed: " + err.Error()}
	}
	matches := matchingCodexRollouts(rollouts, threadID)
	switch len(matches) {
	case 0:
		return codexAssociation{ParentStatus: codexStatusMissing, Basis: codexAssociationBasis, Detail: "no rollout has session_meta.id equal to the bound parent thread ID"}
	case 1:
		return codexAssociation{
			ParentStatus:   codexStatusIncluded,
			ParentPath:     matches[0].AbsolutePath,
			ParentSource:   matches[0].HomeRelative,
			ParentThreadID: matches[0].ID,
			ParentChain:    []codexRollout{matches[0]},
			Basis:          codexAssociationBasis,
		}
	default:
		chain, reason := resolveCodexRolloutChain(matches)
		if reason != "" {
			return ambiguousCodexChainAssociation(matches, codexAssociationBasis, reason)
		}
		return codexAssociation{
			ParentStatus:   codexStatusIncluded,
			ParentPath:     chain[0].AbsolutePath,
			ParentSource:   chain[0].HomeRelative,
			ParentThreadID: chain[0].ID,
			ParentChain:    chain,
			Basis:          codexAssociationBasis,
		}
	}
}
