package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestObservationExecutionRetentionNeverEvictsInFlightClaims(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	claim := ObservationExecutionRecord{
		ExecutionID:      "exec-active",
		TaskID:           taskID,
		Operation:        "shadow-eval",
		ParamsDigest:     "active-digest",
		Status:           ObservationExecutionStatusInFlight,
		DeadlineMS:       30_000,
		DecisionRound:    0,
		StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < observationExecutionsRetention+4; index++ {
		record := ObservationExecutionRecord{
			ExecutionID:        fmt.Sprintf("exec-complete-%d", index),
			TaskID:             taskID,
			Operation:          "shadow-eval",
			ParamsDigest:       fmt.Sprintf("digest-%d", index),
			Status:             ObservationExecutionStatusPass,
			DecisionRound:      0,
			StartedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
			CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := st.AppendObservationExecution(record); err != nil {
			t.Fatal(err)
		}
	}
	records, err := st.ObservationExecutions()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != observationExecutionsRetention+1 {
		t.Fatalf("retained records = %d want %d", len(records), observationExecutionsRetention+1)
	}
	foundClaim := false
	for _, record := range records {
		if record.ExecutionID == claim.ExecutionID {
			foundClaim = true
		}
	}
	if !foundClaim {
		t.Fatal("active observation claim was evicted by retention")
	}
	completed := claim
	completed.Status = ObservationExecutionStatusPass
	completed.CompletedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
	if err := st.CompleteObservationExecution(completed); err != nil {
		t.Fatalf("retained in-flight claim could not complete: %v", err)
	}
}

func TestObservationExecutionsRejectTasklessLegacyState(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	legacy := []ObservationExecutionRecord{{
		ExecutionID:        "legacy-exec",
		Operation:          "shadow-eval",
		ParamsDigest:       "legacy-digest",
		Status:             ObservationExecutionStatusPass,
		DecisionRound:      0,
		CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(observationExecutionsStateFile, string(encoded)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ObservationExecutions(); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("taskless legacy observation state must fail closed, got %v", err)
	}
}
