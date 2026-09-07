package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionRotationMarker struct {
	Version        int                              `json:"version"`
	ParentThreadID string                           `json:"parent_thread_id"`
	State          string                           `json:"state"`
	Directive      *SessionRotationDirective        `json:"directive,omitempty"`
	Issued         *SessionRotationIssued           `json:"issued,omitempty"`
	LimitBaseline  *SessionLimitBaseline            `json:"limit_baseline,omitempty"`
	LastEvaluation *SessionRotationEvaluationRecord `json:"last_evaluation,omitempty"`
	UpdatedAt      string                           `json:"updated_at"`
}

type SessionRotationDirective struct {
	DirectiveID string                    `json:"directive_id"`
	TaskID      string                    `json:"task_id"`
	Terminal    string                    `json:"terminal"`
	Epoch       string                    `json:"epoch"`
	Reason      string                    `json:"reason"`
	Evidence    []SessionRotationEvidence `json:"evidence"`
	CreatedAt   string                    `json:"created_at"`
}

type SessionRotationIssued struct {
	BoundThreadID string `json:"bound_thread_id"`
	IssuedAt      string `json:"issued_at"`
}

type SessionRotationEvidence struct {
	Trigger string `json:"trigger"`
	Field   string `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
	Source  string `json:"source,omitempty"`
}

type SessionLimitBaseline struct {
	LimitID        string `json:"limit_id"`
	WindowResetsAt int64  `json:"window_resets_at"`
	UsedPercent    int64  `json:"used_percent"`
	CapturedAt     string `json:"captured_at"`
}

type SessionLimitReading struct {
	LimitID     string
	UsedPercent int64
	ResetsAt    int64
	CapturedAt  time.Time
}

type SessionRotationEvaluationRecord struct {
	TaskID   string `json:"task_id"`
	Terminal string `json:"terminal"`
	Required bool   `json:"required"`
	Reason   string `json:"reason"`
	At       string `json:"at"`
}

type SessionRotationProjection struct {
	ParentThreadID string                           `json:"parent_thread_id,omitempty"`
	State          string                           `json:"state"`
	Directive      *SessionRotationDirective        `json:"directive,omitempty"`
	LastEvaluation *SessionRotationEvaluationRecord `json:"last_evaluation,omitempty"`
	Reason         string                           `json:"reason,omitempty"`
}

type SessionRotationRolloutSignals struct {
	ModelTurns      int
	ToolOutputBytes int64
	Compactions     int
	Source          string
}

type SessionRotationLimitSignals struct {
	WindowReset     bool
	UsedDeltaPoints float64
	LimitID         string
	BaselineUsed    int64
	LiveUsed        int64
	ResetsAt        int64
}

type SessionRotationSignals struct {
	Terminal                string
	AcceptedTasks           int
	CurrentAcceptedRisk     string
	Rollout                 *SessionRotationRolloutSignals
	RolloutUnavailableField string
	RolloutUnavailableSrc   string
	MaterialEvents          int
	MaterialEventsAvailable bool
	MaterialEventSourceIDs  []string
	Limit                   *SessionRotationLimitSignals
	LimitUnavailableFields  []string
	LimitSource             string
}

type SessionRotationDecision struct {
	Required bool
	Reason   string
	Evidence []SessionRotationEvidence
}

type SessionRotationEvaluation struct {
	ParentThreadID      string
	TaskID              string
	Terminal            string
	Decision            SessionRotationDecision
	AcceptedTasks       int
	LimitBaselineUpdate *SessionLimitBaseline
}

const sessionRotationMarkerVersion = 1

const (
	SessionRotationStatePending = "pending"
	SessionRotationStateIssued  = "issued"
)

const (
	SessionRotationTerminalAccept = "accept"
	SessionRotationTerminalNoGo   = "no-go"
)

const (
	SessionRotationReasonHighRisk            = "high-risk"
	SessionRotationReasonCompaction          = "compaction"
	SessionRotationReasonModelTurns          = "model-turns"
	SessionRotationReasonToolOutputBytes     = "tool-output-bytes"
	SessionRotationReasonRepeatedEvents      = "repeated-events"
	SessionRotationReasonLimitWindow         = "limit-window"
	SessionRotationReasonDefaultTwoTasks     = "default-two-tasks"
	SessionRotationReasonEvidenceUnavailable = "evidence-unavailable"
)

const (
	SessionRotationEvidenceFieldRolloutAssociation = "rollout_association"
	SessionRotationEvidenceFieldRolloutScan        = "rollout_scan"
	SessionRotationEvidenceFieldMaterialEvents     = "material_events"
	SessionRotationEvidenceFieldLimitLive          = "limit_live"
	SessionRotationEvidenceFieldLimitBaseline      = "limit_baseline"
	SessionRotationEvidenceFieldLimitWindow        = "limit_window"
)

const (
	SessionRotationProjectionPending     = "pending"
	SessionRotationProjectionNotRequired = "not-required"
	SessionRotationProjectionUnavailable = "unavailable"
)

const (
	sessionRotationDefaultAcceptedTasks  = 2
	sessionRotationModelTurnsThreshold   = 5
	sessionRotationToolOutputBytesLimit  = 262144
	sessionRotationMaterialEventsTrigger = 2
	sessionRotationLimitDeltaPoints      = 10.0
	sessionRotationDirectory             = "rotation"
)

func (s *StateStore) SessionRotationMarkerPath(threadID string) string {
	return s.Path(filepath.Join(sessionRotationDirectory, threadID+".json"))
}

func (s *StateStore) LoadSessionRotationMarker(threadID string) (*SessionRotationMarker, error) {
	if !ValidUUIDFormat(threadID) {
		return nil, fmt.Errorf("session rotation markerの親thread IDが不正です: %s", threadID)
	}
	data, err := os.ReadFile(s.SessionRotationMarkerPath(threadID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("session rotation markerを読めません: %w", err)
	}
	return decodeSessionRotationMarker(data)
}

func decodeSessionRotationMarker(data []byte) (*SessionRotationMarker, error) {
	var marker SessionRotationMarker
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return nil, fmt.Errorf("session rotation markerのschemaが不正です: %w", err)
	}
	if err := marker.validate(); err != nil {
		return nil, err
	}
	return &marker, nil
}

func (marker *SessionRotationMarker) validate() error {
	if marker.Version != sessionRotationMarkerVersion {
		return fmt.Errorf("session rotation markerのversionが不正です: %d", marker.Version)
	}
	if !ValidUUIDFormat(marker.ParentThreadID) {
		return fmt.Errorf("session rotation markerの親thread IDが不正です: %s", marker.ParentThreadID)
	}
	if err := marker.validateLifecycle(); err != nil {
		return err
	}
	if marker.Directive != nil {
		if err := marker.Directive.validate(); err != nil {
			return err
		}
	}
	if marker.LimitBaseline != nil {
		if err := marker.LimitBaseline.validate(); err != nil {
			return err
		}
	}
	if marker.LastEvaluation != nil {
		if marker.LastEvaluation.TaskID == "" || marker.LastEvaluation.At == "" {
			return fmt.Errorf("session rotation markerのlast_evaluationが不完全です")
		}
	}
	if marker.UpdatedAt == "" {
		return fmt.Errorf("session rotation markerにupdated_atがありません")
	}
	return nil
}

func (marker *SessionRotationMarker) validateLifecycle() error {
	switch marker.State {
	case "":
		if marker.Directive != nil || marker.Issued != nil {
			return fmt.Errorf("directive未発行のsession rotation markerにdirective/issued recordがあります: %s", marker.State)
		}
	case SessionRotationStatePending:
		if marker.Directive == nil {
			return fmt.Errorf("pending session rotation markerにdirectiveがありません")
		}
		if marker.Issued != nil {
			return fmt.Errorf("pending session rotation markerにissued recordがあります")
		}
	case SessionRotationStateIssued:
		if marker.Issued == nil || !ValidUUIDFormat(marker.Issued.BoundThreadID) {
			return fmt.Errorf("issued session rotation markerにbound thread IDがありません")
		}
	default:
		return fmt.Errorf("session rotation markerのstateが不正です: %s", marker.State)
	}
	return nil
}

func (directive *SessionRotationDirective) validate() error {
	if !ValidGeneratedUUID(directive.DirectiveID) {
		return fmt.Errorf("session rotation directiveのIDが不正です: %s", directive.DirectiveID)
	}
	if directive.TaskID == "" || directive.CreatedAt == "" {
		return fmt.Errorf("session rotation directiveにtask IDまたはcreated_atがありません")
	}
	if directive.Terminal != SessionRotationTerminalAccept && directive.Terminal != SessionRotationTerminalNoGo {
		return fmt.Errorf("session rotation directiveのterminalが不正です: %s", directive.Terminal)
	}
	if directive.Epoch != directive.TaskID+":"+directive.Terminal {
		return fmt.Errorf("session rotation directiveのepochがtask/terminalと一致しません: %s", directive.Epoch)
	}
	if !sessionRotationReasonKnown(directive.Reason) {
		return fmt.Errorf("session rotation directiveのreasonが不正です: %s", directive.Reason)
	}
	return nil
}

func (baseline *SessionLimitBaseline) validate() error {
	if baseline.LimitID == "" || baseline.WindowResetsAt <= 0 {
		return fmt.Errorf("session rotation limit baselineが不完全です: %#v", baseline)
	}
	if baseline.UsedPercent < 0 || baseline.UsedPercent > 100 || baseline.CapturedAt == "" {
		return fmt.Errorf("session rotation limit baselineが不正です: %#v", baseline)
	}
	return nil
}

func sessionRotationReasonKnown(reason string) bool {
	switch reason {
	case SessionRotationReasonHighRisk,
		SessionRotationReasonCompaction,
		SessionRotationReasonModelTurns,
		SessionRotationReasonToolOutputBytes,
		SessionRotationReasonRepeatedEvents,
		SessionRotationReasonLimitWindow,
		SessionRotationReasonDefaultTwoTasks,
		SessionRotationReasonEvidenceUnavailable:
		return true
	}
	return false
}

func (s *StateStore) writeSessionRotationMarker(marker *SessionRotationMarker) error {
	marker.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("session rotation markerをJSON化できません: %w", err)
	}
	if err := marker.validate(); err != nil {
		return err
	}
	return writeFileAtomic(s.SessionRotationMarkerPath(marker.ParentThreadID), append(data, '\n'), 0o600)
}

func (s *StateStore) SaveSessionLimitBaseline(threadID string, reading SessionLimitReading) error {
	marker, err := s.LoadSessionRotationMarker(threadID)
	if err != nil {
		return err
	}
	if marker == nil {
		marker = &SessionRotationMarker{
			Version:        sessionRotationMarkerVersion,
			ParentThreadID: threadID,
		}
	}
	if marker.LimitBaseline != nil && marker.LimitBaseline.LimitID == reading.LimitID && marker.LimitBaseline.WindowResetsAt == reading.ResetsAt {
		return nil
	}
	marker.LimitBaseline = sessionLimitBaselineFromReading(reading)
	return s.writeSessionRotationMarker(marker)
}

func sessionLimitBaselineFromReading(reading SessionLimitReading) *SessionLimitBaseline {
	return &SessionLimitBaseline{
		LimitID:        reading.LimitID,
		WindowResetsAt: reading.ResetsAt,
		UsedPercent:    reading.UsedPercent,
		CapturedAt:     reading.CapturedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *StateStore) MarkSessionRotationIssuedOnBind(boundThreadID string) error {
	if !ValidUUIDFormat(boundThreadID) {
		return fmt.Errorf("session rotationのbind対象thread IDが不正です: %s", boundThreadID)
	}
	entries, err := os.ReadDir(s.Path(sessionRotationDirectory))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("session rotation markersを読めません: %w", err)
	}
	for _, entry := range entries {
		threadID := sessionRotationMarkerThreadID(entry.Name())
		if threadID == "" || threadID == boundThreadID {
			continue
		}
		marker, err := s.LoadSessionRotationMarker(threadID)
		if err != nil {
			return err
		}
		if marker == nil || marker.State != SessionRotationStatePending {
			continue
		}
		marker.State = SessionRotationStateIssued
		marker.Issued = &SessionRotationIssued{
			BoundThreadID: boundThreadID,
			IssuedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := s.writeSessionRotationMarker(marker); err != nil {
			return err
		}
	}
	return nil
}

func sessionRotationMarkerThreadID(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if !ValidUUIDFormat(base) {
		return ""
	}
	return base
}

func (s *StateStore) ProjectSessionRotation(threadID string) (SessionRotationProjection, error) {
	if threadID == "" {
		return SessionRotationProjection{
			State:  SessionRotationProjectionUnavailable,
			Reason: "parent-thread-identity-unbound",
		}, nil
	}
	if !ValidUUIDFormat(threadID) {
		return SessionRotationProjection{}, fmt.Errorf("session rotationの親thread IDが不正です: %s", threadID)
	}
	marker, err := s.LoadSessionRotationMarker(threadID)
	if err != nil {
		return SessionRotationProjection{}, err
	}
	if marker == nil {
		return SessionRotationProjection{
			ParentThreadID: threadID,
			State:          SessionRotationProjectionUnavailable,
			Reason:         "no-session-rotation-evaluation",
		}, nil
	}
	projection := SessionRotationProjection{
		ParentThreadID: threadID,
		State:          SessionRotationProjectionNotRequired,
		LastEvaluation: marker.LastEvaluation,
	}
	if marker.State == SessionRotationStatePending {
		projection.State = SessionRotationProjectionPending
		projection.Directive = marker.Directive
	}
	return projection, nil
}

func (s *StateStore) commitSessionRotation(evaluation *SessionRotationEvaluation) error {
	if evaluation == nil {
		return nil
	}
	if !ValidUUIDFormat(evaluation.ParentThreadID) {
		return fmt.Errorf("session rotationには有効な親Codex thread identityが必要です: %s", evaluation.ParentThreadID)
	}
	if evaluation.TaskID == "" {
		return fmt.Errorf("session rotationには現在task IDが必要です")
	}
	marker, err := s.LoadSessionRotationMarker(evaluation.ParentThreadID)
	if err != nil {
		return err
	}
	if marker == nil {
		marker = &SessionRotationMarker{
			Version:        sessionRotationMarkerVersion,
			ParentThreadID: evaluation.ParentThreadID,
		}
	}
	if evaluation.LimitBaselineUpdate != nil {
		marker.LimitBaseline = evaluation.LimitBaselineUpdate
	}
	marker.LastEvaluation = &SessionRotationEvaluationRecord{
		TaskID:   evaluation.TaskID,
		Terminal: evaluation.Terminal,
		Required: evaluation.Decision.Required,
		Reason:   evaluation.Decision.Reason,
		At:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if evaluation.Decision.Required && marker.State != SessionRotationStatePending {
		directiveID, err := NewUUID()
		if err != nil {
			return err
		}
		marker.State = SessionRotationStatePending
		marker.Issued = nil
		marker.Directive = &SessionRotationDirective{
			DirectiveID: directiveID,
			TaskID:      evaluation.TaskID,
			Terminal:    evaluation.Terminal,
			Epoch:       evaluation.TaskID + ":" + evaluation.Terminal,
			Reason:      evaluation.Decision.Reason,
			Evidence:    evaluation.Decision.Evidence,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		}
	}
	return s.writeSessionRotationMarker(marker)
}

func DecideSessionRotation(signals SessionRotationSignals) SessionRotationDecision {
	decision := SessionRotationDecision{Evidence: []SessionRotationEvidence{}}
	sessionRotationHighRiskTrigger(signals, &decision)
	sessionRotationRolloutTriggers(signals, &decision)
	sessionRotationMaterialEventTrigger(signals, &decision)
	sessionRotationLimitTrigger(signals, &decision)
	sessionRotationDefaultTrigger(signals, &decision)
	if decision.Required {
		return decision
	}
	unavailable := sessionRotationUnavailableEvidence(signals)
	if len(unavailable) == 0 {
		return decision
	}
	return SessionRotationDecision{
		Required: true,
		Reason:   SessionRotationReasonEvidenceUnavailable,
		Evidence: unavailable,
	}
}

func sessionRotationRequire(decision *SessionRotationDecision, evidence SessionRotationEvidence) {
	decision.Required = true
	decision.Reason = evidence.Trigger
	decision.Evidence = append(decision.Evidence, evidence)
}

func sessionRotationHighRiskTrigger(signals SessionRotationSignals, decision *SessionRotationDecision) {
	if signals.Terminal != SessionRotationTerminalAccept || signals.CurrentAcceptedRisk != "HIGH" {
		return
	}
	sessionRotationRequire(decision, SessionRotationEvidence{
		Trigger: SessionRotationReasonHighRisk,
		Field:   "accepted_terminal_risk",
		Value:   signals.CurrentAcceptedRisk,
	})
}

func sessionRotationRolloutTriggers(signals SessionRotationSignals, decision *SessionRotationDecision) {
	if signals.Rollout == nil {
		return
	}
	if signals.Rollout.Compactions >= 1 {
		sessionRotationRequire(decision, SessionRotationEvidence{
			Trigger: SessionRotationReasonCompaction,
			Field:   "compactions",
			Value:   fmt.Sprintf("%d", signals.Rollout.Compactions),
			Source:  signals.Rollout.Source,
		})
	}
	if signals.Rollout.ModelTurns > sessionRotationModelTurnsThreshold {
		sessionRotationRequire(decision, SessionRotationEvidence{
			Trigger: SessionRotationReasonModelTurns,
			Field:   "model_turns",
			Value:   fmt.Sprintf("%d", signals.Rollout.ModelTurns),
			Source:  signals.Rollout.Source,
		})
	}
	if signals.Rollout.ToolOutputBytes >= sessionRotationToolOutputBytesLimit {
		sessionRotationRequire(decision, SessionRotationEvidence{
			Trigger: SessionRotationReasonToolOutputBytes,
			Field:   "tool_output_bytes",
			Value:   fmt.Sprintf("%d", signals.Rollout.ToolOutputBytes),
			Source:  signals.Rollout.Source,
		})
	}
}

func sessionRotationMaterialEventTrigger(signals SessionRotationSignals, decision *SessionRotationDecision) {
	if !signals.MaterialEventsAvailable || signals.MaterialEvents < sessionRotationMaterialEventsTrigger {
		return
	}
	sessionRotationRequire(decision, SessionRotationEvidence{
		Trigger: SessionRotationReasonRepeatedEvents,
		Field:   "material_events",
		Value:   fmt.Sprintf("%d", signals.MaterialEvents),
	})
}

func sessionRotationLimitTrigger(signals SessionRotationSignals, decision *SessionRotationDecision) {
	if signals.Limit == nil || signals.Limit.WindowReset || signals.Limit.UsedDeltaPoints < sessionRotationLimitDeltaPoints {
		return
	}
	sessionRotationRequire(decision, SessionRotationEvidence{
		Trigger: SessionRotationReasonLimitWindow,
		Field:   "used_percent_delta",
		Value:   fmt.Sprintf("%.3f", signals.Limit.UsedDeltaPoints),
		Source:  signals.LimitSource,
	})
}

func sessionRotationDefaultTrigger(signals SessionRotationSignals, decision *SessionRotationDecision) {
	if signals.Terminal != SessionRotationTerminalAccept || signals.AcceptedTasks < sessionRotationDefaultAcceptedTasks {
		return
	}
	sessionRotationRequire(decision, SessionRotationEvidence{
		Trigger: SessionRotationReasonDefaultTwoTasks,
		Field:   "accepted_tasks",
		Value:   fmt.Sprintf("%d", signals.AcceptedTasks),
	})
}

func sessionRotationUnavailableEvidence(signals SessionRotationSignals) []SessionRotationEvidence {
	unavailable := []SessionRotationEvidence{}
	if signals.Rollout == nil && signals.RolloutUnavailableField != "" {
		unavailable = append(unavailable, SessionRotationEvidence{
			Trigger: SessionRotationReasonEvidenceUnavailable,
			Field:   signals.RolloutUnavailableField,
			Source:  signals.RolloutUnavailableSrc,
		})
	}
	if !signals.MaterialEventsAvailable {
		unavailable = append(unavailable, SessionRotationEvidence{
			Trigger: SessionRotationReasonEvidenceUnavailable,
			Field:   SessionRotationEvidenceFieldMaterialEvents,
		})
	}
	if signals.Limit == nil {
		for _, field := range signals.LimitUnavailableFields {
			unavailable = append(unavailable, SessionRotationEvidence{
				Trigger: SessionRotationReasonEvidenceUnavailable,
				Field:   field,
				Source:  signals.LimitSource,
			})
		}
	}
	return unavailable
}
