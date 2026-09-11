package app

import (
	"fmt"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func seedParentReviewStateForTest(t *testing.T, st *state.StateStore, taskID string) {
	t.Helper()
	value := fmt.Sprintf(`{"version":1,"task_id":%q}`, taskID)
	if err := st.Write("parent-review-state.json", value); err != nil {
		t.Fatal(err)
	}
}
