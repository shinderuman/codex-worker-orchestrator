package parentactioncmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestObservationExecuteCommandRejectsEvictedTerminalIdentity(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	original := dispatchObservationExecutionForAction
	t.Cleanup(func() { dispatchObservationExecutionForAction = original })
	calls := 0
	dispatchObservationExecutionForAction = func(_ context.Context, _ config.AppConfig, _ *state.StateStore, _ observationExecutionPlan, _ string) observationExecutionOutcome {
		calls++
		return observationExecutionOutcome{Status: state.ObservationExecutionStatusPass, ExitSource: "target"}
	}
	payload := func(deadline int) string {
		return fmt.Sprintf("OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: %d\n", deadline)
	}
	for i := 0; i < 18; i++ {
		token := stageObservationPayload(t, cfg, payload(30000+i))
		var output bytes.Buffer
		if err := execute(cfg, []string{actionObservationExecute, token}, &output, nil); err != nil {
			t.Fatalf("admit %d: %v", i, err)
		}
	}
	records, err := st.ObservationExecutions()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 16 || records[0].ExecutionID == "" {
		t.Fatalf("terminal display retention=%d", len(records))
	}
	if calls != 18 {
		t.Fatalf("dispatch count=%d", calls)
	}
	token := stageObservationPayload(t, cfg, payload(30000))
	if err := execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil); err == nil || !strings.Contains(err.Error(), "再実行") {
		t.Fatalf("evicted terminal replay not refused: %v", err)
	}
	if calls != 18 {
		t.Fatalf("replayed subprocess was invoked: count=%d", calls)
	}
}
