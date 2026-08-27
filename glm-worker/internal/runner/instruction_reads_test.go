package runner

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestClaudeRunnerReportsWorkerInstructionReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"/tmp/.codex/instructions/worker/go.md"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"# Go rules","is_error":false}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"}","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}}`,
		"",
	}, "\n")
	runner, _, _ := newStreamFixtureRunner(t, writeStreamFixtureClaude(t, stream))
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
		t.Fatalf("instruction reads = %v", result.InstructionReads)
	}
}

func TestClaudeRunnerRejectsFailedWorkerInstructionRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"/tmp/.codex/instructions/worker/go.md"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"missing","is_error":true}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"}","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}}`,
		"",
	}, "\n")
	runner, _, _ := newStreamFixtureRunner(t, writeStreamFixtureClaude(t, stream))
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
	if len(result.InstructionReads) != 0 {
		t.Fatalf("failed read counted as instruction evidence: %v", result.InstructionReads)
	}
}

func TestWorkerInstructionReadRejectsUnrelatedRead(t *testing.T) {
	instructionDir := "/tmp/.codex/instructions/worker"
	if name, ok := workerInstructionReadName(
		"Read",
		[]byte(`{"file_path":"/repo/instructions/worker/go.md"}`),
		instructionDir,
	); ok || name != "" {
		t.Fatalf("repo-local fake instruction must not match: %q %v", name, ok)
	}
	if name, ok := workerInstructionReadName(
		"Read",
		[]byte(`{"file_path":"/tmp/.codex/instructions/worker/go.md"}`),
		instructionDir,
	); !ok || name != "go.md" {
		t.Fatalf("configured instruction read did not match: %q %v", name, ok)
	}
	if name, ok := workerInstructionReadName(
		"Bash",
		[]byte(`{"command":"cat ~/.codex/instructions/worker/go.md"}`),
		instructionDir,
	); ok || name != "" {
		t.Fatalf("bash must not count as verified Read: %q %v", name, ok)
	}
}
