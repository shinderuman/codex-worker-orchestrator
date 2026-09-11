package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const standaloneValidationFile = "validation-standalone.jsonl"

const (
	ValidationExitSourceTarget  = "target-process"
	ValidationExitSourceWrapper = "wrapper-synthesized"
	ValidationExitSourceUnknown = "unknown"
)

const (
	ValidationGateClassTest      = "test"
	ValidationGateClassLint      = "lint"
	ValidationGateClassBuild     = "build"
	ValidationGateClassTypecheck = "typecheck"
	ValidationGateClassUnknown   = "unknown"
)

const (
	ValidationAttemptInitial = "initial"
	ValidationAttemptRetry   = "retry"
)

func ValidationGateClass(suite string) string {
	switch suite {
	case "go-test", "go-test-race":
		return ValidationGateClassTest
	case "go-vet", "harnesslint", "commentlint":
		return ValidationGateClassLint
	case "go-build":
		return ValidationGateClassBuild
	case "tsc":
		return ValidationGateClassTypecheck
	default:
		return ValidationGateClassUnknown
	}
}

func ValidationSnapshotID(head, indexDigest, worktreeDigest string) string {
	if head == "" || indexDigest == "" || worktreeDigest == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(head + "\x00" + indexDigest + "\x00" + worktreeDigest))
	return hex.EncodeToString(sum[:])
}

func (s *StateStore) RecordValidation(source, form, scope, result string, exitCode int, exitSource string, durationMS int64, evidence string) {
	s.RecordValidationEvent(TaskValidationEvent{
		Source:     source,
		Form:       form,
		GateClass:  ValidationGateClass(form),
		Suite:      form,
		Phase:      source,
		Attempt:    ValidationAttemptInitial,
		Scope:      scope,
		Result:     result,
		ExitCode:   exitCode,
		ExitSource: exitSource,
		DurationMS: durationMS,
		Evidence:   evidence,
	})
}

func (s *StateStore) RecordValidationEvent(validation TaskValidationEvent) {
	if validation.GateClass == "" {
		validation.GateClass = ValidationGateClass(validation.Form)
	}
	if validation.Suite == "" {
		validation.Suite = validation.Form
	}
	if validation.Attempt == "" {
		validation.Attempt = ValidationAttemptInitial
	}
	record := TaskEventRecord{
		Timestamp:  time.Now().UTC(),
		Kind:       "validation",
		Validation: &validation,
	}
	if taskID := s.ReadOr("task.id", ""); taskID != "" {
		record.TaskID = taskID
		validation.Attribution = "task"
		record.Validation = &validation
		if err := s.AppendTaskEvent(record); err != nil {
			WarnTaskEventSkip("validation追記", err)
		}
		return
	}

	record.Version = taskEventLogVersion
	validation.Attribution = "standalone"
	record.Validation = &validation
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
