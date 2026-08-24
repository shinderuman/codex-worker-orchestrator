package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isolationStateFileは--isolateが元repo state dirへ保存する隔離記録。現在taskの
// 割り込み実行checkoutをmachine-readableに指し、resume時の統合経路判定と--statusの
// 観測に使う。新task開始・resetでtask状態と一緒に消える。
const isolationStateFile = "isolation.json"

// isolationOriginStateFileは--isolateが隔離先worktree側state dirへ保存する出自記録。
// 隔離先で--statusを実行した親Codexが元task・復帰対象を取り違えないための対称な確認で、
// 隔離task自身の開始(StartNewTask)では消さない。
const isolationOriginStateFile = "isolation.origin.json"

// isolationRecordVersionは隔離記録2種の現在version。
const isolationRecordVersion = 1

// IsolationRecordは元repo側へ保存する隔離checkout記録。
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

// IsolationOriginは隔離先worktree側へ保存する出自記録。
type IsolationOrigin struct {
	Version        int    `json:"version"`
	IsolationID    string `json:"isolation_id"`
	OriginRepoRoot string `json:"origin_repo_root"`
	OriginTaskID   string `json:"origin_task_id"`
	Branch         string `json:"branch"`
	CreatedAt      string `json:"created_at"`
}

// ErrNoIsolationRecordは隔離記録が存在しないことを表す sentinel。
var ErrNoIsolationRecord = errors.New("isolation record is not available")

func (s *StateStore) SaveIsolationRecord(record IsolationRecord) error {
	record.Version = isolationRecordVersion
	return writeJSONStateFile(s.Path(isolationStateFile), record)
}

// LoadIsolationRecordは元repo側の隔離記録を読む。不在はErrNoIsolationRecordへ正規化し、
// 隔離経路を一度も使っていないtaskと区別なしに扱えるようにする。
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

// LoadIsolationOriginは隔離先state dirの出自記録を読む。不在はErrNoIsolationRecordへ
// 正規化し、隔離経路でない通常repoと構造上同じ扱いにできるようにする。
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

// ResolveBranchTipはrepoRoot内でbranch名を局所branch(refs/heads/)に限定して解決し、
// tip commit hashを返す。branch不在・曖昧参照はerrorにし、隔離記録の実質検証と
// --isolateの冪等再観測がstale記録をfail closedへ扱えるようにする。
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
