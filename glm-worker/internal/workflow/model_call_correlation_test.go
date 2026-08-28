package workflow

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildModelCallLogKeepsRunnerCallID(t *testing.T) {
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "call-id-test",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Workflow{state: st}
	now := time.Now().UTC()
	entry := w.buildModelCallLog(
		state.ResumeCheckpoint{Role: state.WorkerRole, Model: "opus", Prompt: "prompt"},
		runner.RunResult{CallID: "call-123", SessionID: "session", Response: "response"},
		now,
		now.Add(time.Second),
		"success",
		"IMPLEMENTED",
		nil,
		"unused",
	)
	if entry.CallID != "call-123" {
		t.Fatalf("model call ID = %q", entry.CallID)
	}
}
