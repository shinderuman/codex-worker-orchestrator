package runner

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildRunArgsUsesExplicitWorkerToolPolicy(t *testing.T) {
	r := &ClaudeRunner{}
	args := r.buildRunArgs(
		state.WorkerRole,
		"task-id",
		"session-id",
		false,
		"worker-model",
		false,
		"high",
		"prompt",
		runInputs{isolationArgs: "{}", schema: "{}"},
	)

	got := toolArgument(args)
	if got != workerTools {
		t.Fatalf("worker --tools = %q, want %q", got, workerTools)
	}
	for _, required := range []string{"Read", "Grep", "Glob", "Bash", "Edit", "Write", "NotebookEdit", "TaskCreate", "TaskUpdate", "TaskOutput"} {
		if !toolListContains(got, required) {
			t.Fatalf("worker tool %q is missing from %q", required, got)
		}
	}
	for _, forbidden := range []string{"Agent", "DefinitelyNewClaudeTool"} {
		if toolListContains(got, forbidden) {
			t.Fatalf("worker tool %q must not be implicitly available: %q", forbidden, got)
		}
	}
}

func toolArgument(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--tools" {
			return args[i+1]
		}
	}
	return ""
}

func toolListContains(tools, target string) bool {
	for _, tool := range strings.Split(tools, ",") {
		if tool == target {
			return true
		}
	}
	return false
}
