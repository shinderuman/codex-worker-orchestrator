package workflow

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildModelCallLogCarriesContextWindowDeclaration(t *testing.T) {
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "context-window-test",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Workflow{state: st}
	now := time.Now().UTC()
	entry := w.buildModelCallLog(
		state.ResumeCheckpoint{Role: state.WorkerRole, Model: "opus", Prompt: "prompt"},
		runner.RunResult{
			SessionID:                         "session",
			Response:                          "response",
			ResolvedModelID:                   "glm-5.3",
			ConfiguredAutoCompactWindowTokens: 500_000,
			KnownModelContextWindowTokens:     1_000_000,
			DeclaredMaxContextWindowTokens:    1_000_000,
			ContextWindowSource:               "zai-model-spec",
		},
		now,
		now.Add(time.Second),
		"success",
		"IMPLEMENTED",
		nil,
		"unused",
	)
	if entry.ResolvedModelID != "glm-5.3" || entry.ConfiguredAutoCompactWindowTokens != 500_000 ||
		entry.KnownModelContextWindowTokens != 1_000_000 || entry.DeclaredMaxContextWindowTokens != 1_000_000 ||
		entry.ContextWindowSource != "zai-model-spec" {
		t.Fatalf("context declaration = %#v", entry)
	}
}
