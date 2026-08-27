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
		`{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"}","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}}`,
		"",
	}, "\n")
	runner, _, _ := newStreamFixtureRunner(t, writeStreamFixtureClaude(t, stream))
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

func TestWorkerInstructionReadRejectsUnrelatedRead(t *testing.T) {
	if name, ok := workerInstructionReadName(
		"Read",
		[]byte(`{"file_path":"/repo/docs/go.md"}`),
	); ok || name != "" {
		t.Fatalf("unexpected match: %q %v", name, ok)
	}
	if name, ok := workerInstructionReadName(
		"Bash",
		[]byte(`{"command":"cat ~/.codex/instructions/worker/go.md"}`),
	); ok || name != "" {
		t.Fatalf("bash must not count as verified Read: %q %v", name, ok)
	}
}
