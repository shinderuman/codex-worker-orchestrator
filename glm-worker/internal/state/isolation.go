package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const isolationStateFile = "isolation.json"

const isolationOriginStateFile = "isolation.origin.json"

const isolationRecordVersion = 1

type IsolationRecord struct {
	Version        int    `json:"version"`
	IsolationID    string `json:"isolation_id"`
	Worktree       string `json:"worktree"`
	Branch         string `json:"branch"`
	CreatedAt      string `json:"created_at"`
	OriginTaskID   string `json:"origin_task_id"`
	OriginRepoRoot string `json:"origin_repo_root"`
	OriginHead     string `json:"origin_head"`
}

type IsolationOrigin struct {
	Version        int    `json:"version"`
	IsolationID    string `json:"isolation_id"`
	OriginRepoRoot string `json:"origin_repo_root"`
	OriginTaskID   string `json:"origin_task_id"`
	Branch         string `json:"branch"`
	CreatedAt      string `json:"created_at"`
}

var ErrNoIsolationRecord = errors.New("isolation record is not available")

func (s *StateStore) SaveIsolationRecord(record IsolationRecord) error {
	record.Version = isolationRecordVersion
	return writeJSONStateFile(s.Path(isolationStateFile), record)
}

func (s *StateStore) LoadIsolationRecord() (IsolationRecord, error) {
	var record IsolationRecord
	if err := readJSONStateFile(s.Path(isolationStateFile), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return IsolationRecord{}, ErrNoIsolationRecord
		}
		return IsolationRecord{}, err
	}
	if record.Version != isolationRecordVersion {
		return IsolationRecord{}, fmt.Errorf("unsupported isolation record version: %d", record.Version)
	}
	return record, nil
}

func (s *StateStore) SaveIsolationOrigin(origin IsolationOrigin) error {
	origin.Version = isolationRecordVersion
	return writeJSONStateFile(s.Path(isolationOriginStateFile), origin)
}

func (s *StateStore) LoadIsolationOrigin() (IsolationOrigin, error) {
	var origin IsolationOrigin
	if err := readJSONStateFile(s.Path(isolationOriginStateFile), &origin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return IsolationOrigin{}, ErrNoIsolationRecord
		}
		return IsolationOrigin{}, err
	}
	if origin.Version != isolationRecordVersion {
		return IsolationOrigin{}, fmt.Errorf("unsupported isolation origin version: %d", origin.Version)
	}
	return origin, nil
}

func writeJSONStateFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("isolation記録をJSON化できません: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func ResolveBranchTip(repoRoot string, branch string) (string, error) {
	ref := "refs/heads/" + branch
	output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", ref+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --verify %s^{commit}: %w: %s", ref, err, strings.TrimSpace(string(output)))
	}
	tip := strings.TrimSpace(string(output))
	if tip == "" {
		return "", fmt.Errorf("branch %sのtipが空です", branch)
	}
	return tip, nil
}

func readJSONStateFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("isolation記録を読めません: %w", err)
	}
	return nil
}
