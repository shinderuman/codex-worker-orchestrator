package state

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestObservationTaskRetirementPreservesUnresolvedAndRolledBackFence(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	round, err := st.ObservationExecutionRound()
	if err != nil {
		t.Fatal(err)
	}
	claim := observationFenceClaim(t, st, "retirement", "retained", round)
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err == nil {
		t.Fatal("unresolved observation was discarded by task rotation")
	}
	if err := st.ResolveObservationExecutionIndeterminate(claim.ExecutionID, "dispatch stopped", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.Path(observationExecutionsStateFile))
	if err != nil {
		t.Fatal(err)
	}
	injectOneNewTaskWriteFailure(t, st.Path(observationExecutionsStateFile))
	if _, err := st.StartNewTask(); err == nil {
		t.Fatal("failed document initialization committed a new task")
	}
	after, err := os.ReadFile(st.Path(observationExecutionsStateFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rollback lost the previous observation fence: %v", err)
	}
	if taskID, err := st.TaskID(); err != nil || taskID != claim.TaskID {
		t.Fatalf("rollback changed task authority: %q %v", taskID, err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteObservationExecution(oldToComplete(claim)); err == nil {
		t.Fatal("retired task completion was adopted by the new task")
	}
}

func TestObservationTaskRetirementRecoversOnlyAfterDurableHorizon(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	round, err := st.ObservationExecutionRound()
	if err != nil {
		t.Fatal(err)
	}
	claim := observationFenceClaim(t, st, "expired-retirement", "retained", round)
	claim.StartedAtRFC3339 = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	newTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := st.readObservationDocument()
	if err != nil || doc.TaskID != newTaskID || doc.Round != 0 || len(doc.Fence) != 0 || len(doc.Records) != 0 {
		t.Fatalf("recovered task did not initialize a fresh document: %+v %v", doc, err)
	}
	if err := st.BeginObservationExecution(claim); err == nil {
		t.Fatal("old task authority survived retirement")
	}
}
