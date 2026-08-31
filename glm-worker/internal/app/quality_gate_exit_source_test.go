package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateProcessOutcomeClassifiesExitSource(t *testing.T) {
	cases := []struct {
		name       string
		runErr     error
		wantStatus string
		wantSource string
	}{
		{name: "pass", runErr: nil, wantStatus: qualityGateStatusPass, wantSource: state.ValidationExitSourceTarget},
		{name: "target-failure", runErr: targetExitError(t, 0), wantStatus: qualityGateStatusFail, wantSource: state.ValidationExitSourceTarget},
		{name: "interrupted", runErr: interruptedExitError(t), wantStatus: qualityGateStatusInterrupted, wantSource: state.ValidationExitSourceUnknown},
		{name: "launch-failure", runErr: errors.New("fork/exec /go: no such file or directory"), wantStatus: qualityGateStatusFail, wantSource: state.ValidationExitSourceWrapper},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, source := qualityGateProcessOutcome(tc.runErr)
			if status != tc.wantStatus || source != tc.wantSource {
				t.Fatalf("outcome = %s/%s want %s/%s", status, source, tc.wantStatus, tc.wantSource)
			}
		})
	}
}

func targetExitError(t *testing.T, _ int) error {
	t.Helper()
	command := exec.Command("sh", "-c", "exit 3")
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("exit errorが取得できません: %v", err)
	}
	return err
}

func TestQualityGateLogWriteFailureKeepsTargetExitSource(t *testing.T) {
	cases := []struct {
		name         string
		runErr       error
		wantExitCode int
		wantExitSrc  string
	}{
		{
			name:         "target-failure-keeps-target-exit-source",
			runErr:       targetExitError(t, 0),
			wantExitCode: 3,
			wantExitSrc:  state.ValidationExitSourceTarget,
		},
		{
			name:         "pass-synthesizes-wrapper-exit-source",
			runErr:       nil,
			wantExitCode: 1,
			wantExitSrc:  state.ValidationExitSourceWrapper,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, st := newQualityGateEnv(t)
			runID := strings.Repeat("c", 32)
			if err := os.MkdirAll(filepath.Join(st.Path(qualityGateRunDirectory), runID, qualityGateRunLog), 0o700); err != nil {
				t.Fatal(err)
			}
			record := qualityGateRunRecord{
				ValidationRunID: runID,
				Form:            "go-test",
				Repository:      "/repo",
				StartedAt:       time.Now().Add(-time.Second).UTC(),
				Status:          qualityGateStatusRunning,
			}

			final, err := completeQualityGateRun(st, record, []byte("gate output\n"), tc.runErr)
			if err != nil {
				t.Fatal(err)
			}
			if final.Status != qualityGateStatusFail {
				t.Fatalf("status = %s want %s", final.Status, qualityGateStatusFail)
			}
			if final.ExitCode != tc.wantExitCode {
				t.Fatalf("exit code = %d want %d", final.ExitCode, tc.wantExitCode)
			}
			if final.ExitSource != tc.wantExitSrc {
				t.Fatalf("exit source = %q want %q", final.ExitSource, tc.wantExitSrc)
			}
			if final.Log != "" {
				t.Fatalf("log = %q want empty", final.Log)
			}
		})
	}
}

func interruptedExitError(t *testing.T) error {
	t.Helper()
	command := exec.Command("sh", "-c", "sleep 5")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	return command.Wait()
}

func TestQualityGateValidationEventCarriesTargetExitSource(t *testing.T) {
	useInlineQualityGateRunner(t)
	failFlagPath := filepath.Join(t.TempDir(), "fail-flag")
	shimDir, _ := writeQualityGateGoShim(t, failFlagPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOFLAGS", "-exec=/bin/sh")
	_, st := newQualityGateEnv(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}

	var passOut bytes.Buffer
	if err := runQualityGate("go-test", st, &passOut); err != nil {
		t.Fatalf("pass実行が失敗しました: %v", err)
	}
	if err := os.WriteFile(failFlagPath, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	var failOut bytes.Buffer
	if err := runQualityGate("go-test-race", st, &failOut); err == nil {
		t.Fatal("fail実行がerrorを返しませんでした")
	}

	events := readQualityGateValidationEvents(t, st, taskID)
	if len(events) != 2 {
		t.Fatalf("validation events = %#v", events)
	}
	if events[0].Result != qualityGateStatusPass || events[0].ExitSource != state.ValidationExitSourceTarget || events[0].ExitCode != 0 {
		t.Fatalf("pass event = %#v", events[0])
	}
	if events[1].Result != qualityGateStatusFail || events[1].ExitSource != state.ValidationExitSourceTarget || events[1].ExitCode == 0 {
		t.Fatalf("fail event = %#v", events[1])
	}
}

func TestQualityGateInterruptedReconcileMarksExitSourceUnknown(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("5", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualityGateStatusRunning,
		RunnerPID:       2147483647,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := printQualityGateRun(st, runID, false, &stdout); err != nil {
		t.Fatal(err)
	}
	var got qualityGateRunRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != qualityGateStatusInterrupted || got.ExitCode != -1 {
		t.Fatalf("record = %+v", got)
	}
	if got.ExitSource != state.ValidationExitSourceUnknown {
		t.Fatalf("exit source = %q want %q", got.ExitSource, state.ValidationExitSourceUnknown)
	}
}

func readQualityGateValidationEvents(t *testing.T, st *state.StateStore, taskID string) []state.TaskValidationEvent {
	t.Helper()
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var events []state.TaskValidationEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if record.Kind == "validation" && record.Validation != nil {
			events = append(events, *record.Validation)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}
