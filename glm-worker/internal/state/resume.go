package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ResumeStage string

type ResumeStopKind string

type GuardRefState struct {
	Name     string `json:"name"`
	ObjectID string `json:"object_id"`
	Symref   string `json:"symref,omitempty"`
}

type GuardRefChange struct {
	Name   string         `json:"name"`
	Before *GuardRefState `json:"before,omitempty"`
	After  *GuardRefState `json:"after,omitempty"`
}

type ResumeCheckpoint struct {
	Version                 int                             `json:"version"`
	Stage                   ResumeStage                     `json:"stage"`
	Phase                   string                          `json:"phase"`
	Role                    SessionRole                     `json:"role"`
	Model                   string                          `json:"model"`
	ReadOnly                bool                            `json:"read_only"`
	Effort                  string                          `json:"effort,omitempty"`
	Prompt                  string                          `json:"prompt"`
	OriginalPrompt          string                          `json:"original_prompt,omitempty"`
	Request                 string                          `json:"request"`
	Decision                string                          `json:"decision,omitempty"`
	ActivatedRuleFiles      []string                        `json:"activated_rule_files,omitempty"`
	DecisionBoundaryApplied bool                            `json:"decision_boundary_applied,omitempty"`
	ParentValidation        *packet.ParentValidationRequest `json:"parent_validation,omitempty"`
	ExecutionMilestoneID    string                          `json:"execution_milestone_id,omitempty"`

	WorkerResult    *packet.Result `json:"worker_result,omitempty"`
	CompletedResult *packet.Result `json:"completed_result,omitempty"`
	ReviewNumber    int            `json:"review_number,omitempty"`
	AutoFixes       int            `json:"auto_fixes,omitempty"`
	StopKind        ResumeStopKind `json:"stop_kind"`
	ResetAtCST      string         `json:"reset_at_cst,omitempty"`
	ResetAtRFC3339  string         `json:"reset_at_rfc3339,omitempty"`

	ResultCorrection bool `json:"result_correction,omitempty"`

	RiskFloorReemit bool `json:"risk_floor_reemit,omitempty"`

	ReportOnly bool `json:"report_only"`

	EffectiveRisk       string `json:"effective_risk,omitempty"`
	EffectiveRiskSource string `json:"effective_risk_source,omitempty"`

	ProviderUnavailableClassification string    `json:"provider_unavailable_classification,omitempty"`
	ProviderUnavailableProbes         int       `json:"provider_unavailable_probes,omitempty"`
	ProviderUnavailableStartedAt      time.Time `json:"provider_unavailable_started_at,omitempty"`

	GuardFailure             string           `json:"guard_failure,omitempty"`
	GuardRefBeforeDigest     string           `json:"guard_ref_before_digest,omitempty"`
	GuardRefAfterDigest      string           `json:"guard_ref_after_digest,omitempty"`
	GuardRefStopDigest       string           `json:"guard_ref_stop_digest,omitempty"`
	GuardRefChanges          []GuardRefChange `json:"guard_ref_changes,omitempty"`
	GuardRefChangesTruncated bool             `json:"guard_ref_changes_truncated,omitempty"`

	QualityGateFailure string `json:"quality_gate_failure,omitempty"`

	QualitySurfaceApprovalPending bool `json:"quality_surface_approval_pending,omitempty"`

	// StopParentFiles is an in-memory derived view of StopGitSnapshot.ParentFiles.
	// It is never persisted and must not be treated as a second authority.
	StopParentFiles *ParentFileStates `json:"-"`

	StopGitSnapshot *GitSnapshot `json:"stop_git_snapshot,omitempty"`

	StopDirtyFiles []StopDirtyFile `json:"stop_dirty_files"`
}

const (
	resumeStateFile    = "resume-state.json"
	resumeStateVersion = 6
)

const (
	ResumeStageWorker  ResumeStage = "worker"
	ResumeStageReview  ResumeStage = "reviewer"
	ResumeStageAutoFix ResumeStage = "auto-fix"
)

const (
	ResumeStopNone                ResumeStopKind = ""
	ResumeStopRateLimited         ResumeStopKind = "rate-limited"
	ResumeStopProviderUnavailable ResumeStopKind = "provider-unavailable"
	ResumeStopInterrupted         ResumeStopKind = "interrupted"
	ResumeStopGuardRecoverable    ResumeStopKind = "guard-recoverable"
	ResumeStopQualityGate         ResumeStopKind = "quality-gate-recoverable"
)

var ErrNoResumeCheckpoint = errors.New("resumable task is not available")

func (kind ResumeStopKind) Valid() bool {
	switch kind {
	case ResumeStopNone,
		ResumeStopRateLimited,
		ResumeStopProviderUnavailable,
		ResumeStopInterrupted,
		ResumeStopGuardRecoverable,
		ResumeStopQualityGate:
		return true
	default:
		return false
	}
}

func (kind ResumeStopKind) IsStopped() bool {
	return kind != ResumeStopNone && kind.Valid()
}

func (kind ResumeStopKind) TaskStatus() TaskStatus {
	switch kind {
	case ResumeStopRateLimited:
		return TaskStatusRateLimited
	case ResumeStopProviderUnavailable:
		return TaskStatusProviderUnavailable
	case ResumeStopInterrupted:
		return TaskStatusInterrupted
	case ResumeStopGuardRecoverable:
		return TaskStatusGuardRecoverable
	case ResumeStopQualityGate:
		return TaskStatusQualityGateRecoverable
	default:
		return TaskStatusActive
	}
}

func (kind ResumeStopKind) ResumeSource() string {
	switch kind {
	case ResumeStopRateLimited:
		return "rate-limit"
	case ResumeStopProviderUnavailable:
		return "provider-unavailable"
	case ResumeStopInterrupted:
		return "user-interrupt"
	case ResumeStopGuardRecoverable:
		return "guard-recovery"
	case ResumeStopQualityGate:
		return "quality-gate-recovery"
	default:
		return ""
	}
}

func (checkpoint ResumeCheckpoint) IsStopped() bool {
	return checkpoint.StopKind.IsStopped()
}

func (checkpoint *ResumeCheckpoint) SetStopKind(kind ResumeStopKind) {
	checkpoint.clearStopPayload()
	checkpoint.StopKind = kind
}

func (checkpoint *ResumeCheckpoint) SetStopRepositoryBoundary(snapshot GitSnapshot) {
	checkpoint.StopGitSnapshot = &snapshot
	checkpoint.StopParentFiles = snapshot.ParentFiles
}

func (checkpoint *ResumeCheckpoint) ClearStop() {
	checkpoint.clearStopPayload()
	checkpoint.StopKind = ResumeStopNone
}

func (checkpoint *ResumeCheckpoint) clearStopPayload() {
	clearCompletedResult := checkpoint.StopKind == ResumeStopGuardRecoverable ||
		checkpoint.StopKind == ResumeStopQualityGate ||
		!checkpoint.QualitySurfaceApprovalPending
	checkpoint.ResetAtCST = ""
	checkpoint.ResetAtRFC3339 = ""
	checkpoint.ProviderUnavailableClassification = ""
	checkpoint.ProviderUnavailableProbes = 0
	checkpoint.ProviderUnavailableStartedAt = time.Time{}
	checkpoint.GuardFailure = ""
	checkpoint.GuardRefBeforeDigest = ""
	checkpoint.GuardRefAfterDigest = ""
	checkpoint.GuardRefStopDigest = ""
	checkpoint.GuardRefChanges = nil
	checkpoint.GuardRefChangesTruncated = false
	checkpoint.QualityGateFailure = ""
	if clearCompletedResult {
		checkpoint.CompletedResult = nil
	}
	checkpoint.StopParentFiles = nil
	checkpoint.StopGitSnapshot = nil
	checkpoint.StopDirtyFiles = nil
}

func (checkpoint *ResumeCheckpoint) normalizeStopParentFilesForSave() error {
	if checkpoint.StopGitSnapshot == nil {
		if checkpoint.StopParentFiles == nil {
			return nil
		}
		checkpoint.StopGitSnapshot = &GitSnapshot{ParentFiles: checkpoint.StopParentFiles}
		return nil
	}
	canonical := checkpoint.StopGitSnapshot.ParentFiles
	if checkpoint.StopParentFiles == nil {
		if checkpoint.Stage == ResumeStageReview {
			checkpoint.StopGitSnapshot.ParentFiles = nil
		}
		return nil
	}
	if canonical == nil || !SameParentFileStates(*canonical, *checkpoint.StopParentFiles) {
		return fmt.Errorf("stop parent files diverge from canonical stop_git_snapshot.parent_files")
	}
	return nil
}

func (checkpoint *ResumeCheckpoint) hydrateStopParentFiles() {
	checkpoint.StopParentFiles = nil
	if checkpoint.StopGitSnapshot != nil {
		checkpoint.StopParentFiles = checkpoint.StopGitSnapshot.ParentFiles
	}
}

func (checkpoint ResumeCheckpoint) validateStopState() error {
	if !checkpoint.StopKind.Valid() {
		return fmt.Errorf("unknown resume stop kind: %q", checkpoint.StopKind)
	}
	if checkpoint.QualitySurfaceApprovalPending && checkpoint.StopKind != ResumeStopNone {
		return fmt.Errorf("quality-surface approval checkpoint cannot also carry resume stop kind %q", checkpoint.StopKind)
	}
	if err := checkpoint.validateRateLimitStopPayload(); err != nil {
		return err
	}
	if err := checkpoint.validateProviderStopPayload(); err != nil {
		return err
	}
	if err := checkpoint.validateGuardStopPayload(); err != nil {
		return err
	}
	return checkpoint.validateQualityGateStopPayload()
}

func (checkpoint ResumeCheckpoint) validateRateLimitStopPayload() error {
	if checkpoint.StopKind == ResumeStopRateLimited || (checkpoint.ResetAtCST == "" && checkpoint.ResetAtRFC3339 == "") {
		return nil
	}
	return fmt.Errorf("resume stop payload does not match stop kind %q: rate-limit reset metadata is present", checkpoint.StopKind)
}

func (checkpoint ResumeCheckpoint) validateProviderStopPayload() error {
	if checkpoint.StopKind == ResumeStopProviderUnavailable || (checkpoint.ProviderUnavailableClassification == "" &&
		checkpoint.ProviderUnavailableProbes == 0 && checkpoint.ProviderUnavailableStartedAt.IsZero()) {
		return nil
	}
	return fmt.Errorf("resume stop payload does not match stop kind %q: provider metadata is present", checkpoint.StopKind)
}

func (checkpoint ResumeCheckpoint) validateGuardStopPayload() error {
	if checkpoint.StopKind == ResumeStopGuardRecoverable {
		return nil
	}
	guardEvidence := checkpoint.GuardFailure != "" || checkpoint.GuardRefBeforeDigest != "" ||
		checkpoint.GuardRefAfterDigest != "" || checkpoint.GuardRefStopDigest != "" ||
		len(checkpoint.GuardRefChanges) != 0 || checkpoint.GuardRefChangesTruncated
	guardResult := checkpoint.CompletedResult != nil && !checkpoint.QualitySurfaceApprovalPending && checkpoint.StopKind != ResumeStopQualityGate
	if !guardEvidence && !guardResult {
		return nil
	}
	return fmt.Errorf("resume stop payload does not match stop kind %q: guard-recovery metadata is present", checkpoint.StopKind)
}

func (checkpoint ResumeCheckpoint) validateQualityGateStopPayload() error {
	if checkpoint.StopKind == ResumeStopQualityGate {
		if checkpoint.QualityGateFailure == "" || checkpoint.CompletedResult == nil {
			return fmt.Errorf("quality-gate recovery checkpoint requires the gate failure and the completed worker result")
		}
		return nil
	}
	if checkpoint.QualityGateFailure != "" {
		return fmt.Errorf("resume stop payload does not match stop kind %q: quality-gate metadata is present", checkpoint.StopKind)
	}
	return nil
}

func (s *StateStore) SaveResumeCheckpoint(checkpoint ResumeCheckpoint) error {
	if checkpoint.Model == "" {
		return fmt.Errorf("resume state model is required")
	}
	if err := checkpoint.normalizeStopParentFilesForSave(); err != nil {
		return err
	}
	if err := checkpoint.validateStopState(); err != nil {
		return err
	}
	checkpoint.Version = resumeStateVersion
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("resume stateをJSON化できません: %w", err)
	}

	if err := writeFileAtomic(s.Path(resumeStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("resume stateを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadResumeCheckpoint() (ResumeCheckpoint, error) {
	data, err := os.ReadFile(s.Path(resumeStateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResumeCheckpoint{}, ErrNoResumeCheckpoint
		}
		return ResumeCheckpoint{}, err
	}

	var checkpoint ResumeCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume stateを読めません: %w", err)
	}

	if checkpoint.Version != resumeStateVersion {
		return ResumeCheckpoint{}, fmt.Errorf("unsupported resume state version: %d", checkpoint.Version)
	}

	var explicitKeys struct {
		ReportOnly *bool           `json:"report_only"`
		StopKind   *ResumeStopKind `json:"stop_kind"`
	}
	if err := json.Unmarshal(data, &explicitKeys); err != nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume stateを読めません: %w", err)
	}
	if explicitKeys.ReportOnly == nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume state v6にreport_only keyがありません")
	}
	if explicitKeys.StopKind == nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume state v6にstop_kind keyがありません")
	}
	var persistedKeys map[string]json.RawMessage
	if err := json.Unmarshal(data, &persistedKeys); err != nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume stateを読めません: %w", err)
	}
	if _, legacy := persistedKeys["stop_parent_files"]; legacy {
		return ResumeCheckpoint{}, fmt.Errorf("resume stateを読めません: stop_parent_files is no longer supported; use stop_git_snapshot.parent_files")
	}
	if checkpoint.Model == "" {
		return ResumeCheckpoint{}, fmt.Errorf("resume state model is required")
	}
	if err := checkpoint.validateStopState(); err != nil {
		return ResumeCheckpoint{}, err
	}
	checkpoint.hydrateStopParentFiles()
	return checkpoint, nil
}

func (s *StateStore) ClearResumeCheckpoint() error {
	return s.Remove(resumeStateFile)
}
