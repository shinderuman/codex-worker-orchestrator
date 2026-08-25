package abeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	specVersion      = 1
	runRecordVersion = 1
)

const CanonicalMeasurementBoundary = "親USER_REQUEST/task開始から最終完了までの親Codex全体(委譲前処理、Sol decision/review、fix instruction、final acceptanceを含む)"

const GLMUsageSourceTaskStats = "glm-worker-task-stats"

const CodexUsageSourceAppExport = "codex-app-usage-export"

type Mode string

const (
	ModeDirect Mode = "direct"

	ModeOrchestrated Mode = "orchestrated"
)

type Spec struct {
	Version              int                   `json:"version"`
	ID                   string                `json:"id"`
	UserRequest          string                `json:"user_request"`
	RepoSnapshotCommit   string                `json:"repo_snapshot_commit"`
	InitialWorktree      string                `json:"initial_worktree"`
	CompletionConditions string                `json:"completion_conditions"`
	QualityVerification  string                `json:"quality_verification"`
	CodexModel           string                `json:"codex_model"`
	CodexReasoningEffort string                `json:"codex_reasoning_effort"`
	MeasurementBoundary  string                `json:"measurement_boundary"`
	Isolation            IsolationRequirements `json:"isolation"`
}

type IsolationRequirements struct {
	IndependentSession  bool   `json:"independent_session"`
	IndependentWorktree bool   `json:"independent_worktree"`
	CacheAvoidance      string `json:"cache_avoidance"`
}

type RunRecord struct {
	Version       int           `json:"version"`
	SpecID        string        `json:"spec_id"`
	SpecSHA256    string        `json:"spec_sha256"`
	Mode          Mode          `json:"mode"`
	SessionID     string        `json:"session_id"`
	WorktreePath  string        `json:"worktree_path"`
	Boundary      Boundary      `json:"boundary"`
	RunConditions RunConditions `json:"run_conditions"`
	CodexUsage    CodexUsage    `json:"codex_usage"`
	GLMUsage      GLMUsage      `json:"glm_usage"`
	Quality       Quality       `json:"quality"`
	Proxy         ProxyMetrics  `json:"proxy"`
}

type Boundary struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

type RunConditions struct {
	RepoSnapshotCommit   string `json:"repo_snapshot_commit"`
	InitialWorktree      string `json:"initial_worktree"`
	CodexModel           string `json:"codex_model"`
	CodexReasoningEffort string `json:"codex_reasoning_effort"`
}

type CodexUsage struct {
	Source       string `json:"source"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

func (u CodexUsage) Known() bool {
	return u.Source != ""
}

type GLMUsage struct {
	Source                   string `json:"source"`
	TaskID                   string `json:"task_id,omitempty"`
	InputTokens              int64  `json:"input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	ModelCalls               int    `json:"model_calls"`
}

func (u GLMUsage) IsZero() bool {
	return u == GLMUsage{}
}

type Quality struct {
	TestsRun           int    `json:"tests_run"`
	TestFailures       int    `json:"test_failures"`
	HiddenVerification string `json:"hidden_verification"`
	EscapedBugs        int    `json:"escaped_bugs"`
	ScopeViolations    int    `json:"scope_violations"`
}

type ProxyMetrics struct {
	SolPacketBytes      int `json:"sol_packet_bytes"`
	SolDecisionCommands int `json:"sol_decision_commands"`
	SolFixCommands      int `json:"sol_fix_commands"`
	AutoFixRounds       int `json:"auto_fix_rounds"`
}

func SpecSHA256(spec Spec) string {

	data, _ := json.Marshal(spec)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
