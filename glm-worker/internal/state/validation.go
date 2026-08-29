package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const standaloneValidationFile = "validation-standalone.jsonl"

func (s *StateStore) RecordValidation(source, form, scope, result string, exitCode int, durationMS int64, evidence string) {
	validation := &TaskValidationEvent{
		Source:     source,
		Form:       form,
		Scope:      scope,
		Result:     result,
		ExitCode:   exitCode,
		DurationMS: durationMS,
		Evidence:   evidence,
	}
	record := TaskEventRecord{
		Timestamp:  time.Now().UTC(),
		Kind:       "validation",
		Validation: validation,
	}
	if taskID := s.ReadOr("task.id", ""); taskID != "" {
		record.TaskID = taskID
		validation.Attribution = "task"
		if err := s.AppendTaskEvent(record); err != nil {
			WarnTaskEventSkip("validation追記", err)
		}
		return
	}

	record.Version = taskEventLogVersion
	validation.Attribution = "standalone"
	data, err := json.Marshal(record)
	if err != nil {
		WarnTaskEventSkip("standalone validation JSON化", err)
		return
	}
	path := s.Path(standaloneValidationFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		WarnTaskEventSkip("standalone validation directory作成", err)
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		WarnTaskEventSkip("standalone validation追記", err)
		return
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		WarnTaskEventSkip("standalone validation権限設定", err)
		return
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		WarnTaskEventSkip("standalone validation書込み", err)
		return
	}
	if err := file.Close(); err != nil {
		WarnTaskEventSkip("standalone validation close", err)
	}
}
