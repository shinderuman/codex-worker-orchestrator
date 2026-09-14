package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunEntryEmitsOnlyMachineJSONOnStdout(t *testing.T) {
	cfg := newAppConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runEntry(
		[]string{"--stats"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout is not JSON: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "lock busy") || strings.Contains(stdout.String(), "warning") {
		t.Fatalf("stdout contains non-machine diagnostics: %q", stdout.String())
	}
}

func TestRunEntryFailureLeavesStdoutMachineClean(t *testing.T) {
	cfg := newAppConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runEntry(
		[]string{"--resume"},
		func() (config.AppConfig, error) { return cfg, nil },
		instructionSurfaceRunnerFactory,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("resume without a task must fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure leaked non-machine stdout: %q", stdout.String())
	}
}

func TestRunEntrySuppressesLegacyLockDiagnostics(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = runEntry(
		[]string{"--stats"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "lock busy") || strings.Contains(stderr.String(), "lock busy") {
		t.Fatalf("legacy lock diagnostic leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunEntryRoutesTypedErrorToMachineStderr(t *testing.T) {
	cfg := newAppConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runEntry(
		[]string{"--resume"},
		func() (config.AppConfig, error) { return cfg, nil },
		instructionSurfaceRunnerFactory,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("resume without a task must fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure leaked stdout: %q", stdout.String())
	}
	var event machineErrorEvent
	if decodeErr := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &event); decodeErr != nil {
		t.Fatalf("stderr is not machine error JSON: %v: %q", decodeErr, stderr.String())
	}
	if event.Type != "error" || event.Message == "" {
		t.Fatalf("machine error event = %#v", event)
	}
}

func TestRunEntryDoesNotEmitTypedErrorForSuccessfulMachineCommand(t *testing.T) {
	cfg := newAppConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runEntry(
		[]string{"--stats"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful command emitted machine stderr: %q", stderr.String())
	}
}

func TestRunEntryRejectsTrailingMachineStdoutGarbage(t *testing.T) {
	cfg := newAppConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runEntry(
		[]string{"--stats"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout is not exactly one JSON value: %q", stdout.String())
	}
}

func TestDispatchMachineOutputRejectsMultipleJSONValues(t *testing.T) {
	var target bytes.Buffer
	err := flushMachineStdout(&target, []byte("{}\n{}\n"))
	if err == nil || !strings.Contains(err.Error(), "exactly one JSON value") {
		t.Fatalf("multiple JSON values were accepted: %v", err)
	}
	if target.Len() != 0 {
		t.Fatalf("invalid machine stdout was forwarded: %q", target.String())
	}
}

func TestDispatchMachineOutputAcceptsOneJSONValue(t *testing.T) {
	var target bytes.Buffer
	if err := flushMachineStdout(&target, []byte("{\"ok\":true}\n")); err != nil {
		t.Fatal(err)
	}
	if got, want := target.String(), "{\"ok\":true}\n"; got != want {
		t.Fatalf("target = %q want %q", got, want)
	}
}

func TestDispatchMachineOutputAcceptsJSONScalar(t *testing.T) {
	var target bytes.Buffer
	if err := flushMachineStdout(&target, []byte("true\n")); err != nil {
		t.Fatal(err)
	}
	if got, want := target.String(), "true\n"; got != want {
		t.Fatalf("target = %q want %q", got, want)
	}
}

func TestDispatchMachineOutputRejectsNonJSON(t *testing.T) {
	var target bytes.Buffer
	err := flushMachineStdout(&target, []byte("hello\n"))
	if err == nil {
		t.Fatal("non-JSON stdout was accepted")
	}
	if target.Len() != 0 {
		t.Fatalf("invalid machine stdout was forwarded: %q", target.String())
	}
}

func TestDispatchMachineOutputRejectsEmptyOutput(t *testing.T) {
	var target bytes.Buffer
	err := flushMachineStdout(&target, nil)
	if err == nil {
		t.Fatal("empty machine stdout was accepted")
	}
	if target.Len() != 0 {
		t.Fatalf("empty machine stdout was forwarded: %q", target.String())
	}
}

func TestDispatchMachineOutputPreservesValidWhitespace(t *testing.T) {
	var target bytes.Buffer
	if err := flushMachineStdout(&target, []byte(" \n {\"ok\":true}\n\t")); err != nil {
		t.Fatal(err)
	}
	if got, want := target.String(), " \n {\"ok\":true}\n\t"; got != want {
		t.Fatalf("target = %q want %q", got, want)
	}
}

func TestDispatchMachineOutputDoesNotForwardOnWriteFailure(t *testing.T) {
	writer := &failWriter{err: errors.New("write failed")}
	if err := flushMachineStdout(writer, []byte("{}\n")); err == nil {
		t.Fatal("write failure was hidden")
	}
}

func TestDispatchMachineErrorDoesNotForwardOnWriteFailure(t *testing.T) {
	writer := &failWriter{err: errors.New("write failed")}
	if err := writeMachineError(writer, errors.New("boom")); err == nil {
		t.Fatal("write failure was hidden")
	}
}

func TestDispatchMachineErrorEncodesMessage(t *testing.T) {
	var stderr bytes.Buffer
	if err := writeMachineError(&stderr, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	var event machineErrorEvent
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "error" || event.Message != "boom" {
		t.Fatalf("event = %#v", event)
	}
}

func TestDispatchMachineErrorCompactsNewlines(t *testing.T) {
	var stderr bytes.Buffer
	if err := writeMachineError(&stderr, errors.New("line1\nline2")); err != nil {
		t.Fatal(err)
	}
	var event machineErrorEvent
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(event.Message, "\n") {
		t.Fatalf("error message contains newline: %q", event.Message)
	}
}

func TestDispatchMachineOutputBuffersFailure(t *testing.T) {
	var target bytes.Buffer
	err := dispatchMachineOutput(
		Command{Mode: ModeStats},
		config.AppConfig{},
		nil,
		&target,
		io.Discard,
	)
	if err == nil {
		t.Fatal("invalid state config must fail")
	}
	if target.Len() != 0 {
		t.Fatalf("failure leaked buffered output: %q", target.String())
	}
}

func TestDispatchReleasesTypedStatsWarningThroughMachineStderr(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-stats.json"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = run(
		[]string{"--accept"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "{\"accepted\":true}"; got != want {
		t.Fatalf("stdout = %q want %q", got, want)
	}

	lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("machine stderrへ出力された行数 = %d want 1: %q", len(lines), stderr.String())
	}
	var event struct {
		Type    string `json:"type"`
		Scope   string `json:"scope"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("machine stderr行がJSON objectとして解析できません: %v: %q", err, lines[0])
	}
	if event.Type != "warning" || event.Scope != "task_stats" || event.Message == "" || event.Error == "" {
		t.Fatalf("typed warning eventの契約が守られていません: %#v", event)
	}
}

func TestDispatchKeepsMachineStreamsCleanThroughInstallSmokeSuccess(t *testing.T) {
	cfg, _, _, countPath := newInstallSmokeEnv(t)
	writeInstallSmokeChild(t, cfg, "#!/bin/sh\nprintf 'child noise\\n'\nprintf 'child stderr\\n' >&2\nprintf '1' > \"$INSTALL_COUNT_PATH\"\n")
	t.Setenv("INSTALL_COUNT_PATH", countPath)
	var stdout, stderr bytes.Buffer
	if err := run(
		[]string{"--install-smoke"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("install smoke success leaked stderr: %q", stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("install smoke stdout is not JSON: %q", stdout.String())
	}
}

func TestDispatchKeepsMachineStreamsCleanThroughInstallSmokeFailure(t *testing.T) {
	cfg, _, _ := newInstallSmokeEnv(t)
	writeInstallSmokeChild(t, cfg, "#!/bin/sh\nprintf 'child noise\\n'\nprintf 'child stderr\\n' >&2\nexit 17\n")
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--install-smoke"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("install smoke failure was hidden")
	}
	if stdout.Len() != 0 {
		t.Fatalf("install smoke failure leaked stdout: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("install smoke failure did not emit machine stderr")
	}
}

func TestDispatchBuffersDiagnosticsBeforeMachineStdout(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-stats.json"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run(
		[]string{"--accept"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "\"accepted\":true") {
		t.Fatalf("accept stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "\"type\":\"warning\"") {
		t.Fatalf("stats warning was not routed to stderr: %q", stderr.String())
	}
}

func TestMachineOutputDoesNotDoubleWrapTypedErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	wrapped := &machineOutputError{err: fmt.Errorf("typed failure")}
	if err := writeMachineError(&stderr, wrapped); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("typed error leaked stdout: %q", stdout.String())
	}
	var event machineErrorEvent
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.Message != "typed failure" {
		t.Fatalf("event = %#v", event)
	}
}

type failWriter struct {
	err error
}

func (w *failWriter) Write([]byte) (int, error) {
	return 0, w.err
}
