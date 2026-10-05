package state

import (
	"strings"
	"testing"
	"time"
)

func TestRecoverStaleObservationExecutionMarksIndeterminateWithoutReplay(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	claim := ObservationExecutionRecord{
		ExecutionID:      "exec-stale",
		TaskID:           taskID,
		Operation:        "shadow-eval",
		ParamsDigest:     "digest-stale",
		Status:           ObservationExecutionStatusInFlight,
		DeadlineMS:       30_000,
		DecisionRound:    0,
		StartedAtRFC3339: started.Format(time.RFC3339Nano),
	}
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	if err := st.RecoverStaleObservationExecutions(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v", records, err)
	}
	if records[0].Status != ObservationExecutionStatusIndeterminate || records[0].ExitSource != "recovery" || records[0].CompletedAtRFC3339 == "" {
		t.Fatalf("stale claim not resolved as indeterminate: %#v", records[0])
	}
	if !strings.Contains(records[0].Detail, "automatic replay is refused") {
		t.Fatalf("indeterminate detail = %q", records[0].Detail)
	}
	exists, err := st.HasObservationExecution(claim.Operation, claim.ParamsDigest, claim.DecisionRound)
	if err != nil || !exists {
		t.Fatalf("resolved identity stopped suppressing replay: exists=%v err=%v", exists, err)
	}
}

func TestResolveObservationExecutionIndeterminatePreservesIdentity(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	claim := ObservationExecutionRecord{
		ExecutionID:      "exec-boundary",
		TaskID:           taskID,
		Operation:        "go-test",
		ParamsDigest:     "digest-boundary",
		Status:           ObservationExecutionStatusInFlight,
		DeadlineMS:       30_000,
		DecisionRound:    2,
		StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	if err := st.ResolveObservationExecutionIndeterminate(claim.ExecutionID, "boundary changed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v err=%v", records, err)
	}
	got := records[0]
	if got.Status != ObservationExecutionStatusIndeterminate || got.TaskID != claim.TaskID || got.Operation != claim.Operation || got.ParamsDigest != claim.ParamsDigest || got.DecisionRound != claim.DecisionRound {
		t.Fatalf("resolved identity changed: %#v", got)
	}
}
