package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func enterQualityGateGitWorkingDir(t *testing.T) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(initGitRepo(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func writeTestTimingsGoShim(t *testing.T, exitCode int) string {
	t.Helper()
	shimDir := t.TempDir()
	events := strings.Join([]string{
		`{"Action":"start","Package":"example.com/m"}`,
		`{"Action":"run","Package":"example.com/m","Test":"TestFast"}`,
		`{"Action":"pass","Package":"example.com/m","Test":"TestFast","Elapsed":0.2}`,
		`{"Action":"run","Package":"example.com/m","Test":"TestBroken"}`,
		`{"Action":"fail","Package":"example.com/m","Test":"TestBroken","Elapsed":0.05}`,
		`{"Action":"pass","Package":"example.com/m","Elapsed":0.3}`,
	}, "\n")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = env ]; then\n" +
		"  printf '%s\\n' '{\"GOVERSION\":\"go1.25.4\",\"GOOS\":\"darwin\",\"GOARCH\":\"arm64\",\"GOCACHE\":\"/fixture/cache\",\"GOFLAGS\":\"\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat <<'EOF'\n" + events + "\nEOF\nexit " + string(rune('0'+exitCode)) + "\n"
	if exitCode < 0 || exitCode > 9 {
		t.Fatalf("unsupported shim exit code %d", exitCode)
	}
	if err := execfile.Write(filepath.Join(shimDir, "go"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return shimDir
}

func runQualityGateForRecord(t *testing.T, form string, st *state.StateStore) qualitygate.RunRecord {
	t.Helper()
	var out bytes.Buffer
	err := runQualityGate(form, st, &out)
	if err != nil {
		t.Fatalf("quality gate %s failed: %v", form, err)
	}
	var output qualityGateOutput
	if json.Unmarshal(out.Bytes(), &output) != nil || output.ValidationRunID == "" {
		t.Fatalf("quality gate output = %s", out.String())
	}
	record, readErr := qualitygate.Read(st, output.ValidationRunID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return record
}

func TestQualityGateRunPersistsPerTestTimings(t *testing.T) {
	useInlineQualityGateRunner(t)
	enterQualityGateGitWorkingDir(t)
	t.Setenv("PATH", writeTestTimingsGoShim(t, 0)+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)

	record := runQualityGateForRecord(t, "go-test", st)
	if record.Status != qualitygate.StatusPass || record.TestTimings == "" {
		t.Fatalf("pass run record = %#v", record)
	}
	timings := readPersistedTestTimings(t, record.TestTimings)
	expected := qualitygate.TestTimings{
		SchemaVersion:   qualitygate.TestTimingsSchemaVersion,
		ValidationRunID: record.ValidationRunID,
		Form:            "go-test",
		Tests: []qualitygate.TestTiming{
			{Package: "example.com/m", Action: qualitygate.TestTimingActionPass, ElapsedMS: 300},
			{Package: "example.com/m", Test: "TestBroken", Action: qualitygate.TestTimingActionFail, ElapsedMS: 50},
			{Package: "example.com/m", Test: "TestFast", Action: qualitygate.TestTimingActionPass, ElapsedMS: 200},
		},
	}
	if timings.ValidationRunID != expected.ValidationRunID || len(timings.Tests) != len(expected.Tests) {
		t.Fatalf("persisted timings = %#v", timings)
	}
	for index, timing := range timings.Tests {
		if timing != expected.Tests[index] {
			t.Fatalf("persisted timing[%d] = %#v want %#v", index, timing, expected.Tests[index])
		}
	}
}

func TestQualityGateFailedRunStillPersistsPerTestTimings(t *testing.T) {
	useInlineQualityGateRunner(t)
	enterQualityGateGitWorkingDir(t)
	t.Setenv("PATH", writeTestTimingsGoShim(t, 1)+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)

	var out bytes.Buffer
	if err := runQualityGate("go-test", st, &out); err == nil {
		t.Fatal("failing gate run returned success")
	}
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, entry := range entries {
		if entry.IsDir() && qualitygate.ValidRunID(entry.Name()) {
			if runID != "" {
				t.Fatalf("unexpected extra gate run %s beside %s", entry.Name(), runID)
			}
			runID = entry.Name()
		}
	}
	if runID == "" {
		t.Fatal("failing gate run left no run directory")
	}
	record, err := qualitygate.Read(st, runID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != qualitygate.StatusFail || record.TestTimings == "" {
		t.Fatalf("fail run record = %#v", record)
	}
	timings := readPersistedTestTimings(t, record.TestTimings)
	if timings.CountTests() != 2 {
		t.Fatalf("fail run timings = %#v", timings.Tests)
	}
	failed := false
	for _, timing := range timings.Tests {
		if timing.Test == "TestBroken" && timing.Action == qualitygate.TestTimingActionFail {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("fail run lost the failing test timing: %#v", timings.Tests)
	}
}

func TestQualityGatePlainOutputRunPersistsEmptyTimings(t *testing.T) {
	useInlineQualityGateRunner(t)
	enterQualityGateGitWorkingDir(t)
	shimDir := t.TempDir()
	plainShim := "#!/bin/sh\n" +
		"if [ \"$1\" = env ]; then\n" +
		"  printf '%s\\n' '{\"GOVERSION\":\"go1.25.4\",\"GOOS\":\"darwin\",\"GOARCH\":\"arm64\",\"GOCACHE\":\"/fixture/cache\",\"GOFLAGS\":\"\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' 'go shim: plain log output'\n"
	if err := execfile.Write(filepath.Join(shimDir, "go"), []byte(plainShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)

	record := runQualityGateForRecord(t, "go-test", st)
	if record.TestTimings == "" {
		t.Fatalf("plain output run record = %#v", record)
	}
	timings := readPersistedTestTimings(t, record.TestTimings)
	if timings.CountTests() != 0 || len(timings.Tests) != 0 {
		t.Fatalf("plain output run fabricated timings: %#v", timings.Tests)
	}
}

func readPersistedTestTimings(t *testing.T, path string) qualitygate.TestTimings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	timings, err := qualitygate.DecodeTestTimings(data)
	if err != nil {
		t.Fatal(err)
	}
	return timings
}

func TestQualityGateTimingsWriteFailureFinalizesRecord(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		runErr       error
		wantExitCode int
		wantExitSrc  string
	}{
		{
			name:         "pass-synthesizes-wrapper-failure",
			runErr:       nil,
			wantExitCode: 1,
			wantExitSrc:  state.ValidationExitSourceWrapper,
		},
		{
			name:         "target-failure-keeps-target-exit-source",
			runErr:       targetExitError(t, 0),
			wantExitCode: 3,
			wantExitSrc:  state.ValidationExitSourceTarget,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, st := newQualityGateEnv(t)
			runID := strings.Repeat("e", 32)
			if err := os.MkdirAll(filepath.Join(st.Path(qualitygate.RunDirectory), runID, qualitygate.TestTimingsFile), 0o700); err != nil {
				t.Fatal(err)
			}
			record := qualitygate.RunRecord{
				ValidationRunID: runID,
				Form:            "go-test",
				Repository:      "/repo",
				StartedAt:       time.Now().Add(-time.Second).UTC(),
				Status:          qualitygate.StatusRunning,
			}

			final, err := completeQualityGateRun(st, record, []byte("gate output\n"), tc.runErr, nil)
			if err != nil {
				t.Fatalf("timings書込失敗がrecord確定前にerrorへ逃げました: %v", err)
			}
			if final.Status != qualitygate.StatusFail {
				t.Fatalf("status = %s want %s", final.Status, qualitygate.StatusFail)
			}
			if final.ExitCode != tc.wantExitCode {
				t.Fatalf("exit code = %d want %d", final.ExitCode, tc.wantExitCode)
			}
			if final.ExitSource != tc.wantExitSrc {
				t.Fatalf("exit source = %q want %q", final.ExitSource, tc.wantExitSrc)
			}
			if final.TestTimings != "" {
				t.Fatalf("timings path = %q want empty", final.TestTimings)
			}
			if final.Log == "" {
				t.Fatal("timings書込失敗時にlog証跡が失われました")
			}
			logData, readErr := os.ReadFile(final.Log)
			if readErr != nil || !strings.Contains(string(logData), "gate output") {
				t.Fatalf("gate log evidence = %s %v", logData, readErr)
			}
			persisted, readErr := qualitygate.Read(st, runID)
			if readErr != nil || persisted.Status != qualitygate.StatusFail {
				t.Fatalf("finalized record = %#v %v", persisted, readErr)
			}
		})
	}
}

func TestQualityGateTimingsWriteFailureSurvivesReconcile(t *testing.T) {
	t.Parallel()
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("f", 32)
	if err := os.MkdirAll(filepath.Join(st.Path(qualitygate.RunDirectory), runID, qualitygate.TestTimingsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualitygate.StatusRunning,
	}
	final, err := completeQualityGateRun(st, record, []byte("gate output\n"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reconciled, err := reconcileQualityGateRun(st, runID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Status != final.Status || reconciled.ExitSource != final.ExitSource || reconciled.ExitCode != final.ExitCode {
		t.Fatalf("reconcile rewrote the finalized failure: %#v want %#v", reconciled, final)
	}
}
