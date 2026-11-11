package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTerminalRunErrorKeepsScheduledPauseAndRetryExhaustion(t *testing.T) {
	t.Parallel()
	paused := &ScheduledPauseError{ResumeAt: time.Now().Add(time.Hour)}
	parsed := claudeJSONResult{
		Subtype:        subtypeStructuredOutputRetryExhausted,
		TerminalReason: "max attempts",
	}
	err := terminalRunError(RunResult{}, parsed, nil, paused)
	var gotPause *ScheduledPauseError
	if !errors.As(err, &gotPause) || gotPause != paused {
		t.Fatalf("scheduled pause was lost: %v", err)
	}
	var gotStructured *StructuredOutputError
	if !errors.As(err, &gotStructured) || !gotStructured.RetryExhausted() || gotStructured.TerminalReason != "max attempts" {
		t.Fatalf("retry exhaustion diagnostic was lost: %v", err)
	}
}

func TestTerminalRunErrorNonPauseClassificationUnchanged(t *testing.T) {
	t.Parallel()
	processErr := errors.New("process failed")
	parsed := claudeJSONResult{Subtype: subtypeStructuredOutputRetryExhausted}
	err := terminalRunError(RunResult{}, parsed, nil, processErr)
	if !IsStructuredOutputError(err) || errors.Is(err, processErr) {
		t.Fatalf("non-pause retry exhaustion should retain existing precedence: %v", err)
	}
	var paused *ScheduledPauseError
	if errors.As(err, &paused) {
		t.Fatalf("non-pause result classified as scheduled pause: %v", err)
	}

	err = terminalRunError(RunResult{}, claudeJSONResult{}, errors.New("bad JSON"), processErr)
	if err != processErr {
		t.Fatalf("non-pause process error must retain precedence over parse error: %v", err)
	}
	err = terminalRunError(RunResult{}, claudeJSONResult{}, errors.New("bad JSON"), &ScheduledPauseError{ResumeAt: time.Now().Add(time.Hour)})
	if !errors.As(err, &paused) {
		t.Fatalf("scheduled pause must survive a parse failure: %v", err)
	}
}

func TestFinishRunPreservesScheduledPauseOnOutputWriteFailure(t *testing.T) {
	t.Parallel()
	paused := &ScheduledPauseError{ResumeAt: time.Now().Add(time.Hour), Started: true}
	r := &ClaudeRunner{}
	ingester := &streamEventIngester{
		resultLine: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"completed"}`),
	}
	// Writing to an existing directory fails independently of the command's pause.
	_, err := r.finishRun(state.WorkerRole, t.TempDir(), filepath.Join(t.TempDir(), "stderr"),
		ingester, RunResult{}, paused, claudeVersionScope{})
	var gotPause *ScheduledPauseError
	if !errors.As(err, &gotPause) || gotPause != paused {
		t.Fatalf("scheduled pause was replaced by output write error: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("secondary output write error is not inspectable: %v", err)
	}
}

func TestFinishRunNonPauseOutputWriteFailureUnchanged(t *testing.T) {
	t.Parallel()
	processErr := errors.New("process failed")
	r := &ClaudeRunner{}
	ingester := &streamEventIngester{
		resultLine: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"completed"}`),
	}
	_, err := r.finishRun(state.WorkerRole, t.TempDir(), filepath.Join(t.TempDir(), "stderr"),
		ingester, RunResult{}, processErr, claudeVersionScope{})
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || errors.Is(err, processErr) {
		t.Fatalf("non-pause write failure classification changed: %v", err)
	}
}

func TestFinishRunPreservesPauseWhenRetryExhaustedEventArrives(t *testing.T) {
	t.Parallel()
	paused := &ScheduledPauseError{ResumeAt: time.Now().Add(time.Hour), Started: true}
	r := &ClaudeRunner{}
	ingester := &streamEventIngester{
		resultLine: []byte(`{"type":"result","subtype":"error_max_structured_output_retries","is_error":true}`),
	}
	_, err := r.finishRun(state.WorkerRole, filepath.Join(t.TempDir(), "output.log"),
		filepath.Join(t.TempDir(), "stderr"), ingester, RunResult{}, paused, claudeVersionScope{})
	var gotPause *ScheduledPauseError
	var gotStructured *StructuredOutputError
	if !errors.As(err, &gotPause) || gotPause != paused || !errors.As(err, &gotStructured) || !gotStructured.RetryExhausted() {
		t.Fatalf("pause and retry-exhausted errors were not retained: %v", err)
	}
}
