package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const compactBoundaryFixtureLines = `{"type":"system","subtype":"init","session_id":"sess-compact","model":"glm-5.3"}
{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_before","name":"Bash","input":{"command":"go build ./..."}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_before","content":"ok","is_error":false}]}}
{"type":"system","subtype":"compact_boundary"}
{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"/tmp/.codex/instructions/worker/go.md"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"# Go rules","is_error":false}]}}
{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"TASK012-MUST-NOT-MARKER\",\"requirement_coverage\":\"TASK012-REQUIREMENT-MARKER\",\"tests\":\"TASK012-ACCEPTANCE-MARKER\",\"unverified\":\"none\"}","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"保持 TASK012-MUST-NOT-MARKER","requirement_coverage":"保持 TASK012-REQUIREMENT-MARKER","tests":"保持 TASK012-ACCEPTANCE-MARKER","unverified":"none"},"num_turns":3}
`

func TestClaudeRunnerRecordsCompactBoundaryAndPostBoundaryInstructionRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	runner, st, taskID := newStreamFixtureRunner(t, writeStreamFixtureClaude(t, compactBoundaryFixtureLines))
	runner.config.CodexConfigDir = "/tmp/.codex"

	result, err := runner.Run(
		state.WorkerRole,
		"worker-new",
		"worker-model",
		false,
		"high",
		"prompt",
		filepath.Join(t.TempDir(), "result.log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InstructionReads) != 1 || result.InstructionReads[0] != "go.md" {
		t.Fatalf("boundary後のinstruction read = %v", result.InstructionReads)
	}

	records := readTaskEventLines(t, st, taskID)
	boundaries := 0
	for _, record := range records {
		if state.IsCompactionBoundaryEvent(record) {
			boundaries++
			if record.Seq == 0 || record.Timestamp.IsZero() {
				t.Fatalf("boundary recordのseq/timestamp = %d/%v", record.Seq, record.Timestamp)
			}
		}
	}
	if boundaries != 1 {
		t.Fatalf("compact_boundary記録数 = %d: %d件中", boundaries, len(records))
	}
}

func TestClaudeRunnerCompactBoundaryDoesNotBypassStructuredOutputEnforcement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"compact_boundary"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
		"",
	}, "\n")
	runner, st, taskID := newStreamFixtureRunner(t, writeStreamFixtureClaude(t, stream))

	_, err := runner.Run(
		state.WorkerRole,
		"worker-new",
		"worker-model",
		false,
		"high",
		"prompt",
		filepath.Join(t.TempDir(), "result.log"),
	)
	if !IsStructuredOutputError(err) {
		t.Fatalf("StructuredOutputErrorを期待: %v", err)
	}
	records := readTaskEventLines(t, st, taskID)
	if len(records) != 2 || !state.IsCompactionBoundaryEvent(records[0]) {
		t.Fatalf("fail closed経路でもboundaryを含むeventが保存される想定: %+v", records)
	}
}

func TestClaudeRunnerRequirementMarkersSurviveCompactBoundaryAndResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "stream.jsonl")
	if err := os.WriteFile(fixturePath, []byte(compactBoundaryFixtureLines), 0o600); err != nil {
		t.Fatal(err)
	}
	argumentsPath := filepath.Join(dir, "args")
	commandPath := filepath.Join(dir, "fake-claude")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >%q\ncat %q\n", argumentsPath, fixturePath)
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_FILE", argumentsPath)
	runner, st, taskID := newStreamFixtureRunner(t, commandPath)
	runner.config.EnvAllowlist = []string{"GLM_ARGS_FILE"}

	prompt := strings.Join([]string{
		"MODE: NEW_TASK",
		"Contract: TASK012-REQUIREMENT-MARKER",
		"Must not: TASK012-MUST-NOT-MARKER",
		"Acceptance criteria: TASK012-ACCEPTANCE-MARKER",
		"",
	}, "\n")
	markers := []string{"TASK012-REQUIREMENT-MARKER", "TASK012-MUST-NOT-MARKER", "TASK012-ACCEPTANCE-MARKER"}

	first, err := runner.Run(
		state.WorkerRole,
		"worker-new",
		"worker-model",
		false,
		"high",
		prompt,
		filepath.Join(t.TempDir(), "first.log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Resumed {
		t.Fatal("初回callがresume扱いです")
	}
	assertPromptMarkers(t, argumentsPath, markers, false)
	assertStructuredResultMarkers(t, first.StructuredOutput, markers)

	second, err := runner.Run(
		state.WorkerRole,
		"worker-explicit-fix",
		"worker-model",
		false,
		"high",
		prompt,
		filepath.Join(t.TempDir(), "second.log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Resumed {
		t.Fatal("2回目のcallがresumeされていません")
	}
	assertPromptMarkers(t, argumentsPath, markers, true)
	assertStructuredResultMarkers(t, second.StructuredOutput, markers)

	records := readTaskEventLines(t, st, taskID)
	boundaries := 0
	for _, record := range records {
		if state.IsCompactionBoundaryEvent(record) {
			boundaries++
		}
	}
	if boundaries != 2 {
		t.Fatalf("両callのcompact_boundary観測 = %d (records %d)", boundaries, len(records))
	}
}

func assertPromptMarkers(t *testing.T, argumentsPath string, markers []string, resumed bool) {
	t.Helper()
	data, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	arguments := string(data)
	for _, marker := range markers {
		if !strings.Contains(arguments, marker) {
			t.Fatalf("promptへmarker %sが輸送されていません: %q", marker, arguments)
		}
	}
	if strings.Contains(arguments, "--resume") != resumed {
		t.Fatalf("--resume有無 = %v (resumed %v): %q", strings.Contains(arguments, "--resume"), resumed, arguments)
	}
}

func assertStructuredResultMarkers(t *testing.T, structured json.RawMessage, markers []string) {
	t.Helper()
	result, err := packet.ParseStructured(structured)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.ValidateWorkerResult(result); err != nil {
		t.Fatal(err)
	}
	surfaces := []struct {
		surface string
		field   string
		value   string
		marker  string
	}{
		{"requirement", "requirement_coverage", result.RequirementCoverage, markers[0]},
		{"must-not", "summary", result.Summary, markers[1]},
		{"acceptance", "tests", result.Tests, markers[2]},
	}
	for _, surface := range surfaces {
		if !strings.Contains(surface.value, surface.marker) {
			t.Fatalf("compact_boundary後のstructured resultへ%s面が保持されていません: %s = %q", surface.surface, surface.field, surface.value)
		}
	}
}
