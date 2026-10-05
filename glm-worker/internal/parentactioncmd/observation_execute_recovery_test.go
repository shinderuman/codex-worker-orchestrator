package parentactioncmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestObservationExecuteBoundaryFailureResolvesInFlightAsIndeterminate(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	token := stageObservationPayload(t, cfg, "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")

	original := dispatchObservationExecutionForAction
	t.Cleanup(func() { dispatchObservationExecutionForAction = original })
	dispatchObservationExecutionForAction = func(
		_ context.Context,
		_ config.AppConfig,
		current *state.StateStore,
		_ observationExecutionPlan,
		_ string,
	) observationExecutionOutcome {
		lock, err := repolock.Acquire(current.LockPath())
		if err != nil {
			t.Fatal(err)
		}
		if err := current.SetTaskStatus(state.TaskStatusActive); err != nil {
			_ = lock.Close()
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		return observationExecutionOutcome{Status: state.ObservationExecutionStatusFail, ExitSource: "cancelled", ExitCode: 1}
	}

	err := execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "observation execution boundary changed") {
		t.Fatalf("boundary change error = %v", err)
	}
	records, readErr := st.ObservationExecutions()
	if readErr != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v", records, readErr)
	}
	if records[0].Status != state.ObservationExecutionStatusIndeterminate || records[0].CompletedAtRFC3339 == "" {
		t.Fatalf("boundary failure left unresolved claim: %#v", records[0])
	}
}
