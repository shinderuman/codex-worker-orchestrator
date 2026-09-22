package qualitygate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type RunRecord struct {
	ValidationRunID               string     `json:"validation_run_id"`
	Form                          string     `json:"form"`
	Repository                    string     `json:"repository"`
	WorkingDir                    string     `json:"working_dir"`
	Head                          string     `json:"head"`
	IndexDigest                   string     `json:"index_digest"`
	WorktreeDigest                string     `json:"worktree_digest"`
	WorktreeDigestExcludingParent string     `json:"worktree_digest_excluding_parent,omitempty"`
	TaskID                        string     `json:"task_id,omitempty"`
	StartedAt                     time.Time  `json:"started_at"`
	CompletedAt                   *time.Time `json:"completed_at,omitempty"`
	Status                        string     `json:"status"`
	RunnerPID                     int        `json:"runner_pid,omitempty"`
	ExitCode                      int        `json:"exit_code,omitempty"`
	ExitSource                    string     `json:"exit_source,omitempty"`
	DurationMS                    int64      `json:"duration_ms,omitempty"`
	Log                           string     `json:"log,omitempty"`
}

const (
	RunDirectory      = "quality-gate-runs"
	RunFile           = "run.json"
	RunLog            = "gate.log"
	StatusRunning     = "running"
	StatusPass        = "pass"
	StatusFail        = "fail"
	StatusInterrupted = "interrupted"
)

func NewRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("validation run idを生成できません: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func ValidRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil && strings.ToLower(runID) == runID
}

func RunRelativePath(runID string) string {
	return filepath.Join(RunDirectory, runID, RunFile)
}

func Encode(record RunRecord) ([]byte, error) {
	return json.Marshal(record)
}

func Decode(data []byte) (RunRecord, error) {
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func Read(st *state.StateStore, runID string) (RunRecord, error) {
	if !ValidRunID(runID) {
		return RunRecord{}, fmt.Errorf("invalid validation run id")
	}
	data, err := os.ReadFile(st.Path(RunRelativePath(runID)))
	if err != nil {
		return RunRecord{}, err
	}
	return Decode(data)
}

func VerifyTerminalPass(record RunRecord) error {
	if record.Status != StatusPass || record.CompletedAt == nil || record.ExitCode != 0 || record.ExitSource != state.ValidationExitSourceTarget {
		return fmt.Errorf("quality gate run did not complete with target-process PASS")
	}
	if record.Log == "" {
		return fmt.Errorf("quality gate run PASS has no log evidence")
	}
	if _, err := os.Stat(record.Log); err != nil {
		return fmt.Errorf("quality gate log evidence is unavailable: %w", err)
	}
	return nil
}
