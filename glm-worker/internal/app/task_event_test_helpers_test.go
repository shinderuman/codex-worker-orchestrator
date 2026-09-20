package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeTaskEventLines(t *testing.T, st *state.StateStore, _ string, records ...state.TaskEventRecord) {
	t.Helper()
	for _, record := range records {
		if err := st.AppendTaskEvent(record); err != nil {
			t.Fatal(err)
		}
	}
}
