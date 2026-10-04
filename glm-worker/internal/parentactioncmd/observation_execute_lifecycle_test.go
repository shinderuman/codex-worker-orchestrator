package parentactioncmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestObservationExecutePersistsInFlightBeforeUnlockedDispatch(t *testing.T) {
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
		records, err := current.ObservationExecutions()
		if err != nil || len(records) != 1 || records[0].Status != state.ObservationExecutionStatusInFlight {
			t.Fatalf("dispatch did not observe durable in-flight claim: %#v err=%v", records, err)
		}
		lock, err := repolock.Acquire(current.LockPath())
		if err != nil {
			t.Fatalf("repository lock remained held across observation execution: %v", err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		return observationExecutionOutcome{Status: state.ObservationExecutionStatusPass, ExitSource: "target"}
	}

	if err := execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v", records, err)
	}
	if records[0].Status != state.ObservationExecutionStatusPass || records[0].StartedAtRFC3339 == "" || records[0].CompletedAtRFC3339 == "" {
		t.Fatalf("in-flight claim was not resolved to completion: %#v", records[0])
	}
}
