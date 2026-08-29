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

type ResumeCheckpoint struct {
	Version                 int         `json:"version"`
	Stage                   ResumeStage `json:"stage"`
	Phase                   string      `json:"phase"`
	Role                    SessionRole `json:"role"`
	Model                   string      `json:"model"`
	ReadOnly                bool        `json:"read_only"`
	Effort                  string      `json:"effort,omitempty"`
	Prompt                  string      `json:"prompt"`
	OriginalPrompt          string      `json:"original_prompt,omitempty"`
	Request                 string      `json:"request"`
	Decision                string      `json:"decision,omitempty"`
	ActivatedRuleFiles      []string    `json:"activated_rule_files,omitempty"`
	DecisionBoundaryApplied bool        `json:"decision_boundary_applied,omitempty"`
	ParentValidation        string      `json:"parent_validation,omitempty"`

	WorkerResult    *packet.Result `json:"worker_result,omitempty"`
	CompletedResult *packet.Result `json:"completed_result,omitempty"`
	ReviewNumber    int            `json:"review_number,omitempty"`
	AutoFixes       int            `json:"auto_fixes,omitempty"`
	RateLimited     bool           `json:"rate_limited"`
	ResetAtCST      string         `json:"reset_at_cst,omitempty"`
	ResetAtRFC3339  string         `json:"reset_at_rfc3339,omitempty"`

	ResultCorrection bool `json:"result_correction,omitempty"`

	RiskFloorReemit bool `json:"risk_floor_reemit,omitempty"`

	ReportOnly bool `json:"report_only"`

	EffectiveRisk       string `json:"effective_risk,omitempty"`
	EffectiveRiskSource string `json:"effective_risk_source,omitempty"`

	UserInterrupted bool `json:"user_interrupted,omitempty"`

	ProviderUnavailable               bool      `json:"provider_unavailable,omitempty"`
	ProviderUnavailableClassification string    `json:"provider_unavailable_classification,omitempty"`
	ProviderUnavailableProbes         int       `json:"provider_unavailable_probes,omitempty"`
	ProviderUnavailableStartedAt      time.Time `json:"provider_unavailable_started_at,omitempty"`

	GuardRecoverable bool   `json:"guard_recoverable,omitempty"`
	GuardFailure     string `json:"guard_failure,omitempty"`

	StopParentFiles *ParentFileStates `json:"stop_parent_files,omitempty"`

	StopGitSnapshot *GitSnapshot `json:"stop_git_snapshot,omitempty"`

	StopDirtyFiles []StopDirtyFile `json:"stop_dirty_files"`
}

const (
	resumeStateFile    = "resume-state.json"
	resumeStateVersion = 4
)

const (
	ResumeStageWorker  ResumeStage = "worker"
	ResumeStageReview  ResumeStage = "reviewer"
	ResumeStageAutoFix ResumeStage = "auto-fix"
)

var ErrNoResumeCheckpoint = errors.New("resumable task is not available")

func (s *StateStore) SaveResumeCheckpoint(checkpoint ResumeCheckpoint) error {
	if checkpoint.Model == "" {
		return fmt.Errorf("resume state model is required")
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

	var explicitReportOnly struct {
		ReportOnly *bool `json:"report_only"`
	}
	if err := json.Unmarshal(data, &explicitReportOnly); err != nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume stateを読めません: %w", err)
	}
	if explicitReportOnly.ReportOnly == nil {
		return ResumeCheckpoint{}, fmt.Errorf("resume state v4にreport_only keyがありません")
	}
	if checkpoint.Model == "" {
		return ResumeCheckpoint{}, fmt.Errorf("resume state model is required")
	}
	return checkpoint, nil
}

func (s *StateStore) ClearResumeCheckpoint() error {
	return s.Remove(resumeStateFile)
}
