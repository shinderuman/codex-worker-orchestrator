package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

const (
	resumeStateFile    = "resume-state.json"
	resumeStateVersion = 4
)

type ResumeStage string

const (
	ResumeStageWorker  ResumeStage = "worker"
	ResumeStageReview  ResumeStage = "reviewer"
	ResumeStageAutoFix ResumeStage = "auto-fix"
)

// ResumeCheckpointはZ.ai 5h上限中断時に次回再開へ引き継ぐ状態。
type ResumeCheckpoint struct {
	Version        int         `json:"version"`
	Stage          ResumeStage `json:"stage"`
	Phase          string      `json:"phase"`
	Role           SessionRole `json:"role"`
	Model          string      `json:"model"`
	ReadOnly       bool        `json:"read_only"`
	Effort         string      `json:"effort,omitempty"`
	Prompt         string      `json:"prompt"`
	OriginalPrompt string      `json:"original_prompt,omitempty"`
	Request        string      `json:"request"`
	Decision       string      `json:"decision,omitempty"`
	// WorkerResultはworker工程のtyped結果。
	WorkerResult   *packet.Result `json:"worker_result,omitempty"`
	ReviewNumber   int            `json:"review_number,omitempty"`
	AutoFixes      int            `json:"auto_fixes,omitempty"`
	RateLimited    bool           `json:"rate_limited"`
	ResetAtCST     string         `json:"reset_at_cst,omitempty"`
	ResetAtRFC3339 string         `json:"reset_at_rfc3339,omitempty"`
	// ResultCorrectionは意味検証不合格後の修正再依頼を同一sessionで1回だけ実行する工程を表す。
	ResultCorrection bool `json:"result_correction,omitempty"`
	// RiskFloorReemitは同一reviewer sessionへNEEDS_SOL_REVIEW/HIGH再出力を依頼中の工程を表す。
	RiskFloorReemit bool `json:"risk_floor_reemit,omitempty"`
	// ReportOnlyはTARGETS: PACKETの報告再出力専用工程であることを表す。ReadOnly capabilityで
	// 実行し、resume後もsnapshot-report-only-start.jsonを再撮影せず同じ基準として使う。
	// v4からfalseでも明示保存し、report_only欠落checkpointをphase等から推定せずversion gateで
	// 拒否できるようにする。
	ReportOnly bool `json:"report_only"`
	// EffectiveRiskはwrapperがworker原文riskと区別して決定した実効risk("HIGH"/"LOW")。
	// 空文字は旧checkpointなど未計算を表し、resume時に現在stateから安全側へ決定論的に再構成する。
	EffectiveRisk       string `json:"effective_risk,omitempty"`
	EffectiveRiskSource string `json:"effective_risk_source,omitempty"`
	// UserInterruptedは親Codexの--stop要求による安全停止を表す。既存のrate-limited・
	// provider-unavailableとは停止理由が排他で、role/phase/model/session/promptはそのまま
	// 保持し--resumeで同一sessionから再開する。
	UserInterrupted bool `json:"user_interrupted,omitempty"`
	// ProviderUnavailableは一時provider障害の回復がprobe上限・deadlineに到達し、
	// WORKER_ERROR/RATE_LIMITEDとは独立した再開可能停止状態になったことを表す。
	// role/phase/model/session/promptはそのまま保持し、--resumeで同一session/checkpointから再試行する。
	ProviderUnavailable               bool      `json:"provider_unavailable,omitempty"`
	ProviderUnavailableClassification string    `json:"provider_unavailable_classification,omitempty"`
	ProviderUnavailableProbes         int       `json:"provider_unavailable_probes,omitempty"`
	ProviderUnavailableStartedAt      time.Time `json:"provider_unavailable_started_at,omitempty"`
	// StopParentFilesは停止保存時点の親管理implementation metadata集合状態。review resumeの
	// snapshot例外が保存値と現在値の差分だけをwrapper停止期間中の親Codex更新として承認する。
	// 旧binaryのcheckpointは2file形式の本文を持つため読込時に型不一致でfail closedし、旧形式の
	// 停止期間変化を機械識別できないまま承認しない。
	StopParentFiles *ParentFileStates `json:"stop_parent_files,omitempty"`
}

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

// ErrNoResumeCheckpointは--resume対象の再開可能checkpointが存在しない失敗。
var ErrNoResumeCheckpoint = errors.New("resumable task is not available")

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
	// 現versionだけをresume可能入力とする。旧version checkpoint(v3以下・report_only欠落を
	// 含む)のupgrade・推定は行わず、routing前にresume不能として明示終了させる
	// (machine-only dataは恒久migrationの対象にしない)。
	if checkpoint.Version != resumeStateVersion {
		return ResumeCheckpoint{}, fmt.Errorf("unsupported resume state version: %d", checkpoint.Version)
	}
	// v4はreport_only keyの明示存在を要求する。bool zero value falseはkey欠落と区別できない
	// ため、欠落checkpointを通常auto-fix resumeへ誤routingしないよう*boolで存在検証して
	// fail closedとする。非bool値は最初のUnmarshalの型検証で同じく失敗する。
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
