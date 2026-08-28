package runner

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestClaudeRunnerSharesCallIDWithTaskEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	commandPath := writeStreamFixtureClaude(t, streamFixtureLines)
	r, st, taskID := newStreamFixtureRunner(t, commandPath)
	first, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "first prompt", filepath.Join(t.TempDir(), "first.log"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Run(state.WorkerRole, "worker-decision", "worker-model", false, "high", "second prompt", filepath.Join(t.TempDir(), "second.log"))
	if err != nil {
		t.Fatal(err)
	}
	records := readTaskEventLines(t, st, taskID)
	if first.CallID == "" || second.CallID == "" || first.CallID == second.CallID {
		t.Fatalf("RunResult call IDs = %q / %q", first.CallID, second.CallID)
	}
	if len(records) != 8 || records[0].CallID != first.CallID || records[4].CallID != second.CallID {
		t.Fatalf("event/result call IDs = %#v / %q / %q", records, first.CallID, second.CallID)
	}
}
