package app

import (
	"bytes"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRotateInstructionBaselineRequiresWaitingDecision(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir(), StateBase: t.TempDir(), RepoHash: "repo"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := rotateInstructionBaseline(cfg, st, &output); err == nil {
		t.Fatal("rotation was accepted outside waiting-decision")
	}
}

func TestParseRotateInstructionBaselineCommand(t *testing.T) {
	command, err := ParseCommand([]string{"--rotate-instruction-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != modeRotateInstructionBaseline {
		t.Fatalf("mode = %d, want %d", command.Mode, modeRotateInstructionBaseline)
	}
	if _, err := ParseCommand([]string{"--rotate-instruction-baseline", "extra"}); err == nil {
		t.Fatal("rotation command accepted an extra argument")
	}
}
