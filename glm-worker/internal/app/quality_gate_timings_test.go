package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	body := "#!/bin/sh\ncat <<'EOF'\n" + events + "\nEOF\nexit " + string(rune('0'+exitCode)) + "\n"
	if exitCode < 0 || exitCode > 9 {
		t.Fatalf("unsupported shim exit code %d", exitCode)
	}
	if err := os.WriteFile(filepath.Join(shimDir, "go"), []byte(body), 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(shimDir, "go"), []byte("#!/bin/sh\nprintf '%s\\n' 'go shim: plain log output'\n"), 0o755); err != nil {
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
