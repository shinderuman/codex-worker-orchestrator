package parentactioncmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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

func TestObservationExecuteCancelsWhenDecisionBoundaryChanges(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	token := stageObservationPayload(t, cfg, "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")

	started := make(chan struct{})
	original := dispatchObservationExecutionForAction
	t.Cleanup(func() { dispatchObservationExecutionForAction = original })
	dispatchObservationExecutionForAction = func(
		ctx context.Context,
		_ config.AppConfig,
		_ *state.StateStore,
		_ observationExecutionPlan,
		_ string,
	) observationExecutionOutcome {
		close(started)
		select {
		case <-ctx.Done():
			return observationExecutionOutcome{Status: state.ObservationExecutionStatusFail, ExitSource: "cancelled", Detail: ctx.Err().Error(), ExitCode: 1}
		case <-time.After(5 * time.Second):
			t.Fatal("observation context was not cancelled after lifecycle change")
			return observationExecutionOutcome{}
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("observation dispatch did not start")
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatalf("lifecycle mutation could not acquire repository lock: %v", err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "observation execution boundary changed") {
			t.Fatalf("lifecycle change did not fail closed after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observation execution did not cancel promptly after lifecycle change")
	}
}

func TestObservationExecuteCancelsWhenTaskAuthorityChanges(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	token := stageObservationPayload(t, cfg, "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")

	started := make(chan struct{})
	original := dispatchObservationExecutionForAction
	t.Cleanup(func() { dispatchObservationExecutionForAction = original })
	dispatchObservationExecutionForAction = func(
		ctx context.Context,
		_ config.AppConfig,
		_ *state.StateStore,
		_ observationExecutionPlan,
		_ string,
	) observationExecutionOutcome {
		close(started)
		select {
		case <-ctx.Done():
			return observationExecutionOutcome{Status: state.ObservationExecutionStatusFail, ExitSource: "cancelled", Detail: ctx.Err().Error(), ExitCode: 1}
		case <-time.After(5 * time.Second):
			t.Fatal("observation context was not cancelled after task authority change")
			return observationExecutionOutcome{}
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("observation dispatch did not start")
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatalf("task authority mutation could not acquire repository lock: %v", err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# changed observation\n\n## External feasibility\n\nstatus: observation\nassumption: changed producer behavior\n")); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "observation execution boundary changed") {
			t.Fatalf("task authority change did not fail closed after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observation execution did not cancel promptly after task authority change")
	}
}
