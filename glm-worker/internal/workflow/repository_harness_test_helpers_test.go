package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func pinRepositoryHarnessActiveT(t *testing.T, st *state.StateStore) {
	t.Helper()
	if st.Exists(repositoryharness.ActivationStateKey) ||
		(st.TaskStatus() != state.TaskStatusActive && !st.Exists(activeTaskStateKey)) {
		return
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
}

func pinRepositoryHarnessInactiveT(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationInactiveValue); err != nil {
		t.Fatal(err)
	}
}
