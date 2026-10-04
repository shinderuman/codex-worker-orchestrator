package state

import (
	"encoding/json"
	"testing"
	"time"
)

func TestObservationExecutionInFlightPreventsReplayAndCompletesInPlace(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	claim := ObservationExecutionRecord{
		ExecutionID:      "exec-inflight",
		TaskID:           taskID,
		Operation:        "shadow-eval",
		ParamsDigest:     "digest-inflight",
		Status:           ObservationExecutionStatusInFlight,
		DecisionRound:    0,
		StartedAtRFC3339: started,
	}
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	exists, err := st.HasObservationExecution(claim.Operation, claim.ParamsDigest, claim.DecisionRound)
	if err != nil || !exists {
		t.Fatalf("durable in-flight identity did not suppress replay: exists=%v err=%v", exists, err)
	}
	projected, err := st.ObservationExecutionsForRound(0)
	if err != nil || len(projected) != 0 {
		t.Fatalf("in-flight record leaked into completed projection: %#v err=%v", projected, err)
	}
	if err := st.BeginObservationExecution(claim); err == nil {
		t.Fatal("duplicate in-flight execution identity was admitted")
	}

	completed := claim
	completed.Status = ObservationExecutionStatusPass
	completed.CompletedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
	if err := st.CompleteObservationExecution(completed); err != nil {
		t.Fatal(err)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 || records[0].Status != ObservationExecutionStatusPass {
		t.Fatalf("completion did not resolve claim in place: %#v err=%v", records, err)
	}
	projected, err = st.ObservationExecutionsForRound(0)
	if err != nil || len(projected) != 1 || projected[0].ExecutionID != claim.ExecutionID {
		t.Fatalf("completed execution projection = %#v err=%v", projected, err)
	}
}

func TestObservationExecutionIdentityIsCurrentTaskScoped(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	currentTaskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	records := []ObservationExecutionRecord{
		{
			ExecutionID:        "exec-old-task",
			TaskID:             "old-task-id",
			Operation:          "shadow-eval",
			ParamsDigest:       "shared-digest",
			Status:             ObservationExecutionStatusPass,
			DecisionRound:      0,
			CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		},
		{
			ExecutionID:        "exec-current-task",
			TaskID:             currentTaskID,
			Operation:          "shadow-eval",
			ParamsDigest:       "current-digest",
			Status:             ObservationExecutionStatusPass,
			DecisionRound:      0,
			CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(observationExecutionsStateFile, string(encoded)); err != nil {
		t.Fatal(err)
	}
	exists, err := st.HasObservationExecution("shadow-eval", "shared-digest", 0)
	if err != nil || exists {
		t.Fatalf("prior-task record suppressed current task: exists=%v err=%v", exists, err)
	}
	projected, err := st.ObservationExecutionsForRound(0)
	if err != nil || len(projected) != 1 || projected[0].TaskID != currentTaskID {
		t.Fatalf("prior-task record entered current projection: %#v err=%v", projected, err)
	}
}
