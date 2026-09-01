package workflow

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeCleanTaskBaselineState(t *testing.T, st *state.StateStore, baseline string) {
	t.Helper()
	if err := st.Write("baseline-head", baseline); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-status", "clean"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"baseline-index.patch", "baseline-worktree.patch", "baseline-untracked"} {
		if err := os.WriteFile(st.Path(name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
