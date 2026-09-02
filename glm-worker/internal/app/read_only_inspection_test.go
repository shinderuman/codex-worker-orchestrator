package app

import (
	"io"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReadOnlyInspectionDoesNotRequireWritableStateStore(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := st.Path(".")
	if err := os.Chmod(stateRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateRoot, 0o700) })

	for _, args := range [][]string{{"--status"}, {"--handoff"}, {"--stats"}, {"--project-state"}} {
		cmd, err := ParseCommand(args)
		if err != nil {
			t.Fatalf("ParseCommand(%v): %v", args, err)
		}
		if err := Execute(cmd, cfg, nil, io.Discard, io.Discard); err != nil {
			t.Fatalf("Execute(%v) with read-only state dir: %v", args, err)
		}
	}
}
