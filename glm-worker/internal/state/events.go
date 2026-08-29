package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TaskValidationObservation struct {
	Form   string `json:"form"`
	Result string `json:"result,omitempty"`
}

type TaskValidationEvent struct {
	Attribution string `json:"attribution"`
	Source      string `json:"source"`
	Form        string `json:"form"`
	Scope       string `json:"scope,omitempty"`
	Result      string `json:"result"`
	ExitCode    int    `json:"exit_code,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

type TaskBlockSummary struct {
	Type              string                      `json:"type"`
	Name              string                      `json:"name,omitempty"`
	ToolID            string                      `json:"tool_id,omitempty"`
	OperationCategory string                      `json:"operation_category,omitempty"`
	Validation        []TaskValidationObservation `json:"validation,omitempty"`
	Bytes             int                         `json:"bytes"`
	IsError           bool                        `json:"is_error,omitempty"`
	DurationMS        int64                       `json:"duration_ms,omitempty"`
}

type TaskEventUsage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
}

type TaskCompactionSummary struct {
	Trigger                 string `json:"trigger,omitempty"`
	PreTokens               int64  `json:"pre_tokens,omitempty"`
	PostTokens              int64  `json:"post_tokens,omitempty"`
	CumulativeDroppedTokens int64  `json:"cumulative_dropped_tokens,omitempty"`
	DurationMS              int64  `json:"duration_ms,omitempty"`
}

type TaskEventRecord struct {
	Version     int       `json:"version"`
	TaskID      string    `json:"task_id"`
	CallID      string    `json:"call_id"`
	SessionID   string    `json:"session_id,omitempty"`
	Role        string    `json:"role"`
	Phase       string    `json:"phase"`
	ModelAlias  string    `json:"model_alias,omitempty"`
	Resumed     bool      `json:"resumed,omitempty"`
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	Kind        string    `json:"kind"`
	Subtype     string    `json:"subtype,omitempty"`
	SearchQuery string    `json:"search_query,omitempty"`
	SearchPaths []string  `json:"search_paths,omitempty"`

	MessageModel  string                 `json:"message_model,omitempty"`
	Compaction    *TaskCompactionSummary `json:"compaction,omitempty"`
	Blocks        []TaskBlockSummary     `json:"blocks,omitempty"`
	Usage         *TaskEventUsage        `json:"usage,omitempty"`
	Validation    *TaskValidationEvent   `json:"validation,omitempty"`
	IsError       bool                   `json:"is_error,omitempty"`
	DurationMS    int64                  `json:"duration_ms,omitempty"`
	DurationAPIMS int64                  `json:"duration_api_ms,omitempty"`
	NumTurns      int                    `json:"num_turns,omitempty"`
	TotalCostUSD  float64                `json:"total_cost_usd,omitempty"`
}

const (
	ValidationResultPass    = "pass"
	ValidationResultFail    = "fail"
	ValidationResultUnknown = "unknown"
)

const taskEventLogVersion = 1

const retainedTaskEventLogs = 10

const (
	OperationCategorySearch    = "search"
	OperationCategoryTest      = "test"
	OperationCategoryBuild     = "build"
	OperationCategoryFormat    = "format"
	OperationCategoryInstall   = "install"
	OperationCategoryGitRead   = "git-read"
	OperationCategoryGitWrite  = "git-write"
	OperationCategoryFileRead  = "file-read"
	OperationCategoryFileWrite = "file-write"
	OperationCategoryOther     = "other"
)

func (s *StateStore) AppendTaskEvent(record TaskEventRecord) error {
	if record.Version == 0 {
		record.Version = taskEventLogVersion
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("task eventをJSON化できません: %w", err)
	}
	path := s.TaskEventLogPath(record.TaskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func ParseTaskEventLine(data []byte) (TaskEventRecord, error) {
	var record TaskEventRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return TaskEventRecord{}, fmt.Errorf("task eventを読めません: %w", err)
	}
	if record.Version != taskEventLogVersion {
		return TaskEventRecord{}, fmt.Errorf("unsupported task event version: %d", record.Version)
	}
	return record, nil
}

func WarnTaskEventSkip(operation string, err error) {
	writeStatsWarningEvent("event_log", fmt.Sprintf("passive event logの%sに失敗したためevent記録をskipします（task本体へ影響しません）", operation), err)
}

func WarnTaskEventCap(limit int) {
	writeStatsWarningEvent("event_log", fmt.Sprintf("passive event logの追記がcall当たり上限%d件に到達したため以後のevent記録をskipします（task本体へ影響しません）", limit), nil)
}

func (s *StateStore) TaskEventLogPath(taskID string) string {
	return s.Path(filepath.Join("events", taskID+".jsonl"))
}

func (s *StateStore) PruneTaskEventLogs(keep int, currentTaskID string) {
	paths, err := filepath.Glob(s.Path(filepath.Join("events", "*.jsonl")))
	if err != nil {
		WarnTaskEventPrune(err)
		return
	}
	currentLog := s.TaskEventLogPath(currentTaskID)
	type entry struct {
		path  string
		mtime time.Time
	}
	entries := make([]entry, 0, len(paths))
	for _, path := range paths {
		if path == currentLog {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			WarnTaskEventPrune(err)
			continue
		}
		entries = append(entries, entry{path: path, mtime: info.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.After(entries[j].mtime) })
	for index, item := range entries {
		if index < keep {
			continue
		}
		if err := os.Remove(item.path); err != nil {
			WarnTaskEventPrune(err)
		}
		livePath := strings.TrimSuffix(item.path, ".jsonl") + ".live.json"
		if err := os.Remove(livePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			WarnTaskEventPrune(err)
		}
	}
}

func WarnTaskEventPrune(err error) {
	writeStatsWarningEvent("event_log", "旧task event logのretention整理に失敗しました（task本体へ影響しません）", err)
}
