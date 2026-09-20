package machinecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type UsageError struct {
	Message string
}

type NotFoundError struct {
	Message string
}

type StdinPayloadError struct {
	Message string
}

func UsageErrorf(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

func (e *UsageError) Error() string {
	return e.Message
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func (e *StdinPayloadError) Error() string {
	return e.Message
}

func WriteJSON(w io.Writer, value any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func StringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func TaskStatusPtr(status state.TaskStatus) *string {
	switch status {
	case state.TaskStatusActive,
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusAwaitingParentCompletion,
		state.TaskStatusComplete,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusQualityGateRecoverable,
		state.TaskStatusInterrupted,
		state.TaskStatusParked:
		value := string(status)
		return &value
	}
	return nil
}
