package sessionrotation

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const guardRecoverableOutcome = "guard_recoverable"

func EvaluateTerminal(
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
	rolloutSignals(cfg, identity.ThreadID, &signals)
	materialEventSignals(st, taskID, &signals)
	baselineUpdate, err := limitSignals(cfg, st, identity.ThreadID, &signals)
	if err != nil {
		return nil, err
	}
	decision := state.DecideSessionRotation(signals)
	attachMaterialEventSources(st, taskID, signals, &decision)
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

func rolloutSignals(cfg config.AppConfig, threadID string, signals *state.SessionRotationSignals) {
	chain, detail := canonicalRolloutChain(cfg.CodexConfigDir, threadID)
	if detail != "" {
		signals.RolloutUnavailableField = state.SessionRotationEvidenceFieldRolloutAssociation
		signals.RolloutUnavailableSrc = detail
		return
	}
	start := chainStart(chain)
	end := time.Now().UTC()
	activity, err := codexrollout.ScanActivity(chain, start, end)
	if err != nil {
		signals.RolloutUnavailableField = state.SessionRotationEvidenceFieldRolloutScan
		signals.RolloutUnavailableSrc = codexrollout.SourceLabel(chain)
		return
	}
	signals.Rollout = &state.SessionRotationRolloutSignals{
		ModelTurns:      activity.ModelTurns,
		ToolOutputBytes: activity.ToolOutputBytes,
		Compactions:     activity.Compactions,
		Source:          codexrollout.SourceLabel(chain),
	}
}

func canonicalRolloutChain(codexHome, threadID string) ([]codexrollout.Rollout, string) {
	if !state.ValidUUIDFormat(threadID) {
		return nil, "parent Codex thread identity is invalid"
	}
	if !codexrollout.DirExists(codexHome) {
		return nil, "codex home is not present"
	}
	rollouts, err := codexrollout.Scan(codexHome)
	if err != nil {
		return nil, "codex rollout enumeration failed: " + err.Error()
	}
	matches := codexrollout.Matching(rollouts, threadID)
	switch len(matches) {
	case 0:
		return nil, "no rollout has session_meta.id equal to the bound parent thread ID"
	case 1:
		return []codexrollout.Rollout{matches[0]}, ""
	default:
		chain, reason := codexrollout.ResolveChain(matches)
		if reason != "" {
			return nil, fmt.Sprintf("%d rollouts share the stored parent thread ID; %s", len(matches), reason)
		}
		return chain, ""
	}
}

func chainStart(chain []codexrollout.Rollout) time.Time {
	var start time.Time
	for _, member := range chain {
		if member.FirstTimestamp.IsZero() {
			continue
		}
		if start.IsZero() || member.FirstTimestamp.Before(start) {
			start = member.FirstTimestamp
		}
	}
	return start
}

func materialEventSignals(st *state.StateStore, taskID string, signals *state.SessionRotationSignals) {
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		signals.MaterialEventsAvailable = false
		return
	}
	count, ids := materialEvents(logs)
	signals.MaterialEvents = count
	signals.MaterialEventsAvailable = true
	signals.MaterialEventSourceIDs = ids
}

func materialEvents(logs []state.ModelCallLog) (int, []string) {
	seen := make(map[string]bool, len(logs))
	ids := make([]string, 0, 2)
	for _, log := range logs {
		if !materialEvent(log) || seen[log.CallID] {
			continue
		}
		seen[log.CallID] = true
		ids = append(ids, log.CallID)
	}
	return len(ids), ids
}

func materialEvent(log state.ModelCallLog) bool {
	if log.Outcome == guardRecoverableOutcome {
		return true
	}
	if log.CallType != state.CallTypeEvent {
		return false
	}
	if log.Phase == state.ParentPhaseDecision && log.Outcome == state.ParentOutcomeDecision {
		return true
	}
	return log.Phase == state.ParentPhaseFix &&
		log.Outcome == state.ParentOutcomeFix &&
		(log.ParentOrigin == state.ParentOriginGLMReviewer || log.ParentOrigin == state.ParentOriginCodexReview)
}

func limitSignals(
	cfg config.AppConfig,
	st *state.StateStore,
	threadID string,
	signals *state.SessionRotationSignals,
) (*state.SessionLimitBaseline, error) {
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		return nil, err
	}
	snapshot, liveErr := codexlimit.Read(cfg.CodexBin)
	if liveErr != nil {
		signals.LimitUnavailableFields = []string{state.SessionRotationEvidenceFieldLimitLive}
		signals.LimitSource = liveErr.Error()
		return nil, nil
	}
	if snapshot.LimitID == "" || snapshot.FiveHour.UsedPercent == nil || snapshot.FiveHour.ResetsAt == nil {
		signals.LimitUnavailableFields = []string{state.SessionRotationEvidenceFieldLimitLive}
		return nil, nil
	}
	if marker == nil || marker.LimitBaseline == nil {
		signals.LimitUnavailableFields = []string{state.SessionRotationEvidenceFieldLimitBaseline}
		return nil, nil
	}
	baseline := marker.LimitBaseline
	if baseline.LimitID != snapshot.LimitID {
		signals.LimitUnavailableFields = []string{state.SessionRotationEvidenceFieldLimitWindow}
		return nil, nil
	}
	if baseline.WindowResetsAt != *snapshot.FiveHour.ResetsAt {
		return baselineFromSnapshot(snapshot), nil
	}
	signals.Limit = &state.SessionRotationLimitSignals{
		UsedDeltaPoints: float64(*snapshot.FiveHour.UsedPercent - baseline.UsedPercent),
		LimitID:         snapshot.LimitID,
		BaselineUsed:    baseline.UsedPercent,
		LiveUsed:        *snapshot.FiveHour.UsedPercent,
		ResetsAt:        *snapshot.FiveHour.ResetsAt,
	}
	signals.LimitSource = st.SessionRotationMarkerPath(threadID)
	return nil, nil
}

func baselineFromSnapshot(snapshot codexlimit.Snapshot) *state.SessionLimitBaseline {
	return &state.SessionLimitBaseline{
		LimitID:        snapshot.LimitID,
		WindowResetsAt: *snapshot.FiveHour.ResetsAt,
		UsedPercent:    *snapshot.FiveHour.UsedPercent,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func attachMaterialEventSources(st *state.StateStore, taskID string, signals state.SessionRotationSignals, decision *state.SessionRotationDecision) {
	for index := range decision.Evidence {
		if decision.Evidence[index].Trigger != state.SessionRotationReasonRepeatedEvents &&
			decision.Evidence[index].Field != state.SessionRotationEvidenceFieldMaterialEvents {
			continue
		}
		locator := st.ModelCallLogPath(taskID)
		if len(signals.MaterialEventSourceIDs) != 0 {
			locator += ":" + strings.Join(signals.MaterialEventSourceIDs, ",")
		}
		decision.Evidence[index].Source = locator
	}
}
