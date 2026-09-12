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
	Version             int                              `json:"version"`
	ParentThreadID      string                           `json:"parent_thread_id"`
	State               string                           `json:"state"`
	Directive           *SessionRotationDirective        `json:"directive,omitempty"`
	Claim               *SessionRotationClaim            `json:"claim,omitempty"`
	Issued              *SessionRotationIssued           `json:"issued,omitempty"`
	LastCreationOutcome *SessionRotationCreationOutcome  `json:"last_creation_outcome,omitempty"`
	LimitBaseline       *SessionLimitBaseline            `json:"limit_baseline,omitempty"`
	AcceptedTasks       *int                             `json:"accepted_tasks,omitempty"`
	LastEvaluation      *SessionRotationEvaluationRecord `json:"last_evaluation,omitempty"`
	UpdatedAt           string                           `json:"updated_at"`
}

type SessionRotationClaim struct {
	ClaimID          string `json:"claim_id"`
	ClaimantThreadID string `json:"claimant_thread_id"`
	TargetTaskID     string `json:"target_task_id"`
	ClaimedAt        string `json:"claimed_at"`
	BoundThreadID    string `json:"bound_thread_id,omitempty"`
	BoundAt          string `json:"bound_at,omitempty"`
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
	Claim          *SessionRotationClaim            `json:"claim,omitempty"`
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
	Terminal                 string
	AcceptedTasks            int
	AcceptedTasksUnavailable bool
	AcceptedTasksSource      string
	CurrentAcceptedRisk      string
	Rollout                  *SessionRotationRolloutSignals
	RolloutUnavailableField  string
	RolloutUnavailableSrc    string
	MaterialEvents           int
	MaterialEventsAvailable  bool
	MaterialEventSourceIDs   []string
	Limit                    *SessionRotationLimitSignals
	LimitUnavailableFields   []string
	LimitSource              string
}

type SessionRotationDecision struct {
	Required bool
	Reason   string
	Evidence []SessionRotationEvidence
}

type SessionRotationEvaluation struct {
	ParentThreadID           string
	TaskID                   string
	Terminal                 string
	Decision                 SessionRotationDecision
	AcceptedTasks            int
	AcceptedTasksUnavailable bool
	LimitBaselineUpdate      *SessionLimitBaseline
}

const sessionRotationMarkerVersion = 3

const (
	SessionRotationStatePending = "pending"
	SessionRotationStateClaimed = "claimed"
	SessionRotationStateBound   = "bound"
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
	SessionRotationEvidenceFieldAcceptedTasks      = "accepted_tasks"
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
	if err := marker.validateStateRecords(); err != nil {
		return err
	}
	if marker.Claim != nil {
		if !ValidGeneratedUUID(marker.Claim.ClaimID) || !ValidUUIDFormat(marker.Claim.ClaimantThreadID) || !ValidGeneratedUUID(marker.Claim.TargetTaskID) || marker.Claim.ClaimedAt == "" {
			return fmt.Errorf("session rotation claimが不正です")
		}
		if marker.Claim.ClaimantThreadID != marker.ParentThreadID {
			return fmt.Errorf("session rotation claimのclaimant thread IDが親thread IDと一致しません")
		}
	}
	return marker.validateCreationOutcomeAndAcceptedTasks()
}

func (marker *SessionRotationMarker) validateStateRecords() error {
	switch marker.State {
	case "":
		return validateNoSessionRotationRecords(marker)
	case SessionRotationStatePending:
		return validatePendingSessionRotationRecords(marker)
	case SessionRotationStateClaimed:
		return validateClaimedSessionRotationRecords(marker)
	case SessionRotationStateBound:
		return validateBoundSessionRotationRecords(marker)
	case SessionRotationStateIssued:
		return validateIssuedSessionRotationRecords(marker)
	default:
		return fmt.Errorf("session rotation markerのstateが不正です: %s", marker.State)
	}
}

func validateClaimedSessionRotationRecords(marker *SessionRotationMarker) error {
	if marker.Directive == nil || marker.Claim == nil || marker.Claim.BoundThreadID != "" || marker.Issued != nil {
		return fmt.Errorf("claimed session rotation markerのstate recordが不正です")
	}
	return nil
}

func validateBoundSessionRotationRecords(marker *SessionRotationMarker) error {
	if marker.Directive == nil || marker.Claim == nil || !ValidUUIDFormat(marker.Claim.BoundThreadID) || marker.Claim.BoundAt == "" || marker.Issued != nil {
		return fmt.Errorf("bound session rotation markerのstate recordが不正です")
	}
	return nil
}

func validateIssuedSessionRotationRecords(marker *SessionRotationMarker) error {
	if marker.Directive == nil || marker.Claim == nil || marker.Issued == nil || !ValidUUIDFormat(marker.Issued.BoundThreadID) {
		return fmt.Errorf("issued session rotation markerのstate recordが不正です")
	}
	if marker.Claim.BoundThreadID != marker.Issued.BoundThreadID {
		return fmt.Errorf("issued session rotation markerのbound thread IDがclaimと一致しません")
	}
	return nil
}

func validateNoSessionRotationRecords(marker *SessionRotationMarker) error {
	if marker.Directive != nil || marker.Claim != nil || marker.Issued != nil {
		return fmt.Errorf("directive未発行のsession rotation markerにdirective/issued recordがあります: %s", marker.State)
	}
	return nil
}

func validatePendingSessionRotationRecords(marker *SessionRotationMarker) error {
	if marker.Directive == nil {
		return fmt.Errorf("pending session rotation markerにdirectiveがありません")
	}
	if marker.Claim != nil || marker.Issued != nil {
		return fmt.Errorf("pending session rotation markerにclaim/issued recordがあります")
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

func (s *StateStore) ClaimSessionRotation(parentThreadID, directiveID string) (SessionRotationClaim, error) {
	marker, err := s.LoadSessionRotationMarker(parentThreadID)
	if err != nil {
		return SessionRotationClaim{}, err
	}
	if marker == nil || marker.Directive == nil || marker.Directive.DirectiveID != directiveID {
		return SessionRotationClaim{}, fmt.Errorf("session rotation directiveが見つかりません")
	}
	if existing, ok := reusableSessionRotationClaim(marker, parentThreadID); ok {
		return existing, nil
	}
	if marker.State != SessionRotationStatePending {
		return SessionRotationClaim{}, fmt.Errorf("session rotation directiveは既にclaim済みです: %s", marker.State)
	}
	claimID, err := NewUUID()
	if err != nil {
		return SessionRotationClaim{}, err
	}
	targetTaskID, err := NewUUID()
	if err != nil {
		return SessionRotationClaim{}, err
	}
	marker.State = SessionRotationStateClaimed
	marker.Claim = &SessionRotationClaim{ClaimID: claimID, ClaimantThreadID: parentThreadID, TargetTaskID: targetTaskID, ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.writeSessionRotationMarker(marker); err != nil {
		return SessionRotationClaim{}, err
	}
	return *marker.Claim, nil
}

func reusableSessionRotationClaim(marker *SessionRotationMarker, parentThreadID string) (SessionRotationClaim, bool) {
	if marker.State != SessionRotationStateClaimed && marker.State != SessionRotationStateBound {
		return SessionRotationClaim{}, false
	}
	if marker.Claim == nil || marker.Claim.ClaimantThreadID != parentThreadID {
		return SessionRotationClaim{}, false
	}
	return *marker.Claim, true
}

func (s *StateStore) ReleaseSessionRotationClaim(parentThreadID, directiveID, claimID string, result SessionRotationCreationResult) error {
	marker, err := s.LoadSessionRotationMarker(parentThreadID)
	if err != nil {
		return err
	}
	if marker == nil || marker.Directive == nil || marker.Claim == nil || marker.Directive.DirectiveID != directiveID || marker.Claim.ClaimID != claimID {
		return fmt.Errorf("session rotation claimが一致しません")
	}
	if marker.State != SessionRotationStateClaimed {
		return fmt.Errorf("bind済みまたは完了済みのsession rotation claimはreleaseできません: %s", marker.State)
	}
	if err := result.validateForRelease(parentThreadID, directiveID, claimID, marker.Claim.TargetTaskID); err != nil {
		return err
	}
	marker.LastCreationOutcome = newSessionRotationCreationOutcome(result)
	marker.State = SessionRotationStatePending
	marker.Claim = nil
	return s.writeSessionRotationMarker(marker)
}

func (s *StateStore) BindSessionRotationClaim(parentThreadID, directiveID, claimID, boundThreadID string) error {
	if !ValidUUIDFormat(boundThreadID) || boundThreadID == parentThreadID {
		return fmt.Errorf("session rotationのbind対象thread IDが不正です: %s", boundThreadID)
	}
	marker, err := s.LoadSessionRotationMarker(parentThreadID)
	if err != nil {
		return err
	}
	if marker == nil || marker.Directive == nil || marker.Claim == nil || marker.Directive.DirectiveID != directiveID || marker.Claim.ClaimID != claimID {
		return fmt.Errorf("session rotation claimが一致しません")
	}
	if marker.State == SessionRotationStateBound && marker.Claim.BoundThreadID == boundThreadID {
		return nil
	}
	if marker.State != SessionRotationStateClaimed {
		return fmt.Errorf("session rotation claimはbindできません: %s", marker.State)
	}
	marker.State = SessionRotationStateBound
	marker.Claim.BoundThreadID = boundThreadID
	marker.Claim.BoundAt = time.Now().UTC().Format(time.RFC3339Nano)
	return s.writeSessionRotationMarker(marker)
}

func (s *StateStore) AcknowledgeSessionRotationClaim(claimID, boundThreadID string) error {
	marker, err := s.findSessionRotationClaim(claimID)
	if err != nil {
		return err
	}
	if marker.State == SessionRotationStateIssued && marker.Issued != nil && marker.Issued.BoundThreadID == boundThreadID {
		return nil
	}
	if marker.State != SessionRotationStateBound || marker.Claim == nil || marker.Claim.BoundThreadID != boundThreadID {
		return fmt.Errorf("session rotation claimはこのthreadでacknowledgeできません")
	}
	if s.ReadOr("task.id", "") != marker.Claim.TargetTaskID {
		return fmt.Errorf("session rotation claimのtarget taskが開始されていません")
	}
	identity, err := s.CurrentParentCodexIdentity()
	if err != nil || identity.ThreadID != boundThreadID || identity.TaskID != marker.Claim.TargetTaskID {
		return fmt.Errorf("session rotation claimのparent identityがbind対象と一致しません")
	}
	marker.State = SessionRotationStateIssued
	marker.Issued = &SessionRotationIssued{BoundThreadID: boundThreadID, IssuedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	return s.writeSessionRotationMarker(marker)
}

func (s *StateStore) findSessionRotationClaim(claimID string) (*SessionRotationMarker, error) {
	entries, err := os.ReadDir(s.Path(sessionRotationDirectory))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("session rotation claimが見つかりません")
		}
		return nil, fmt.Errorf("session rotation markersを読めません: %w", err)
	}
	var found *SessionRotationMarker
	for _, entry := range entries {
		threadID := sessionRotationMarkerThreadID(entry.Name())
		if threadID == "" {
			continue
		}
		marker, loadErr := s.LoadSessionRotationMarker(threadID)
		if loadErr != nil {
			return nil, loadErr
		}
		if marker != nil && marker.Claim != nil && marker.Claim.ClaimID == claimID {
			if found != nil {
				return nil, fmt.Errorf("session rotation claimが重複しています")
			}
			found = marker
		}
	}
	if found == nil {
		return nil, fmt.Errorf("session rotation claimが見つかりません")
	}
	return found, nil
}

func (s *StateStore) ValidateNewTaskRotation(callerThreadID, claimID string) error {
	_, err := s.AdmitNewTaskRotation(callerThreadID, claimID)
	return err
}

func (s *StateStore) AdmitNewTaskRotation(callerThreadID, claimID string) (bool, error) {
	if claimID != "" {
		return s.admitClaimedSessionRotation(callerThreadID, claimID)
	}
	return s.admitUnclaimedNewTask(callerThreadID)
}

func (s *StateStore) admitClaimedSessionRotation(callerThreadID, claimID string) (bool, error) {
	marker, err := s.findSessionRotationClaim(claimID)
	if err != nil {
		return false, err
	}
	if marker.Claim == nil || marker.Claim.BoundThreadID != callerThreadID {
		return false, fmt.Errorf("session rotation claimまたはbound threadが一致しません")
	}
	currentTaskID := s.ReadOr("task.id", "")
	if marker.State == SessionRotationStateIssued {
		if currentTaskID == marker.Claim.TargetTaskID && s.sessionRotationStartRetryable() {
			return true, nil
		}
		return false, fmt.Errorf("session rotation claimは既に完了しています")
	}
	if marker.State != SessionRotationStateBound {
		return false, fmt.Errorf("session rotation claimは開始に使用できません: %s", marker.State)
	}
	if !s.sessionRotationSourceTaskAdmitted(marker, currentTaskID) {
		return false, fmt.Errorf("session rotation開始中のtask identityが一致しません")
	}
	return currentTaskID == marker.Claim.TargetTaskID, nil
}

func (s *StateStore) sessionRotationSourceTaskAdmitted(marker *SessionRotationMarker, currentTaskID string) bool {
	if currentTaskID == marker.Directive.TaskID || currentTaskID == marker.Claim.TargetTaskID {
		return true
	}
	if currentTaskID == "" {
		return false
	}
	evaluation := marker.LastEvaluation
	if evaluation == nil || evaluation.TaskID != currentTaskID || !evaluation.Required {
		return false
	}
	if evaluation.Terminal != SessionRotationTerminalAccept && evaluation.Terminal != SessionRotationTerminalNoGo {
		return false
	}
	return s.TaskStatus() == TaskStatusComplete
}

func (s *StateStore) sessionRotationStartRetryable() bool {
	if s.TaskStatus() != TaskStatusActive {
		return false
	}
	checkpoint, err := s.LoadResumeCheckpoint()
	return err == nil && checkpoint.Phase == "worker-new" && checkpoint.Role == WorkerRole
}

func (s *StateStore) admitUnclaimedNewTask(callerThreadID string) (bool, error) {
	incomplete, err := s.incompleteSessionRotationTarget(callerThreadID)
	if err != nil {
		return false, err
	}
	if incomplete {
		return false, fmt.Errorf("session rotation開始の再試行にはrotation claimが必要です")
	}
	if err := s.rejectRetiredSessionRotationCaller(callerThreadID); err != nil {
		return false, err
	}
	identity, err := s.CurrentParentCodexIdentity()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	marker, err := s.LoadSessionRotationMarker(identity.ThreadID)
	if err != nil {
		return false, err
	}
	if marker == nil || marker.State == "" || marker.State == SessionRotationStateIssued {
		return false, nil
	}
	return false, fmt.Errorf("pending session rotationをclaim・bindしてから新threadで開始してください: %s", marker.State)
}

func (s *StateStore) rejectRetiredSessionRotationCaller(callerThreadID string) error {
	if !ValidUUIDFormat(callerThreadID) {
		return nil
	}
	marker, err := s.LoadSessionRotationMarker(callerThreadID)
	if err != nil {
		return err
	}
	if marker != nil && marker.State == SessionRotationStateIssued {
		return fmt.Errorf("retired session rotation parentから新taskは開始できません")
	}
	return nil
}

func (s *StateStore) incompleteSessionRotationTarget(callerThreadID string) (bool, error) {
	if !ValidUUIDFormat(callerThreadID) || s.TaskStatus() != TaskStatusActive {
		return false, nil
	}
	entries, err := os.ReadDir(s.Path(sessionRotationDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("session rotation markersを読めません: %w", err)
	}
	currentTaskID := s.ReadOr("task.id", "")
	for _, entry := range entries {
		threadID := sessionRotationMarkerThreadID(entry.Name())
		if threadID == "" {
			continue
		}
		marker, err := s.LoadSessionRotationMarker(threadID)
		if err != nil {
			return false, err
		}
		if marker != nil && sessionRotationMarkerTargets(marker, callerThreadID, currentTaskID) {
			return true, nil
		}
	}
	return false, nil
}

func sessionRotationMarkerTargets(marker *SessionRotationMarker, callerThreadID, taskID string) bool {
	if marker.Claim == nil || marker.Claim.BoundThreadID != callerThreadID || marker.Claim.TargetTaskID != taskID {
		return false
	}
	return marker.State == SessionRotationStateBound || marker.State == SessionRotationStateIssued
}

func (s *StateStore) StartSessionRotationTask(callerThreadID, claimID string) (string, error) {
	marker, err := s.findSessionRotationClaim(claimID)
	if err != nil {
		return "", err
	}
	if marker.Claim == nil || marker.Claim.BoundThreadID != callerThreadID {
		return "", fmt.Errorf("session rotation claimまたはbound threadが一致しません")
	}
	currentTaskID := s.ReadOr("task.id", "")
	if marker.State == SessionRotationStateIssued {
		if currentTaskID == marker.Claim.TargetTaskID && s.sessionRotationStartRetryable() {
			return s.startNewTaskWithID(marker.Claim.TargetTaskID, true)
		}
		return "", fmt.Errorf("session rotation claimは既に完了しています")
	}
	if marker.State != SessionRotationStateBound {
		return "", fmt.Errorf("session rotation claimは開始に使用できません: %s", marker.State)
	}
	if currentTaskID != "" && !s.sessionRotationSourceTaskAdmitted(marker, currentTaskID) {
		return "", fmt.Errorf("session rotation開始中のtask identityが一致しません")
	}
	return s.startNewTaskWithID(marker.Claim.TargetTaskID, currentTaskID == marker.Claim.TargetTaskID)
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
	if marker.State == SessionRotationStatePending || marker.State == SessionRotationStateClaimed || marker.State == SessionRotationStateBound {
		projection.State = marker.State
		projection.Directive = marker.Directive
		projection.Claim = marker.Claim
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
	marker.AcceptedTasks = sessionRotationAcceptedTasksForCommit(evaluation)
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
		if marker.State == SessionRotationStateClaimed || marker.State == SessionRotationStateBound {
			return s.writeSessionRotationMarker(marker)
		}
		directiveID, err := NewUUID()
		if err != nil {
			return err
		}
		marker.State = SessionRotationStatePending
		marker.Claim = nil
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
	if signals.Terminal != SessionRotationTerminalAccept || signals.AcceptedTasksUnavailable || signals.AcceptedTasks < sessionRotationDefaultAcceptedTasks {
		return
	}
	sessionRotationRequire(decision, SessionRotationEvidence{
		Trigger: SessionRotationReasonDefaultTwoTasks,
		Field:   SessionRotationEvidenceFieldAcceptedTasks,
		Value:   fmt.Sprintf("%d", signals.AcceptedTasks),
		Source:  signals.AcceptedTasksSource,
	})
}

func sessionRotationUnavailableEvidence(signals SessionRotationSignals) []SessionRotationEvidence {
	unavailable := []SessionRotationEvidence{}
	if signals.Terminal == SessionRotationTerminalAccept && signals.AcceptedTasksUnavailable {
		unavailable = append(unavailable, SessionRotationEvidence{
			Trigger: SessionRotationReasonEvidenceUnavailable,
			Field:   SessionRotationEvidenceFieldAcceptedTasks,
			Source:  signals.AcceptedTasksSource,
		})
	}
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
