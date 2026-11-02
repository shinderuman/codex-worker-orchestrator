package state

import (
		"testing"
	"time"
)

func TestObservationExecutionInFlightPreventsReplayAndCompletesInPlace(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	round, err := st.ObservationExecutionRound()
	if err != nil { t.Fatal(err) }
	started := time.Now().UTC().Format(time.RFC3339Nano)
	claim := ObservationExecutionRecord{
		ExecutionID:      "exec-inflight",
		TaskID:           taskID,
		Operation:        "shadow-eval",
		ParamsDigest:     "digest-inflight",
		Status:           ObservationExecutionStatusInFlight,
		DeadlineMS:       30_000,
		DecisionRound:    round,
		StartedAtRFC3339: started,
	}
	if err := st.BeginObservationExecution(claim); err != nil {
		t.Fatal(err)
	}
	exists, err := st.HasObservationExecution(claim.Operation, claim.ParamsDigest, claim.DecisionRound)
	if err != nil || !exists {
		t.Fatalf("durable in-flight identity did not suppress replay: exists=%v err=%v", exists, err)
	}
	projected, err := st.ObservationExecutionsForRound(round)
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
	projected, err = st.ObservationExecutionsForRound(round)
	if err != nil || len(projected) != 1 || projected[0].ExecutionID != claim.ExecutionID {
		t.Fatalf("completed execution projection = %#v err=%v", projected, err)
	}
}

func TestObservationExecutionIdentityIsCurrentTaskScoped(t *testing.T) {
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	round,err:=st.ObservationExecutionRound()
	if err!=nil{t.Fatal(err)}
	taskID,err:=st.TaskID()
	if err!=nil{t.Fatal(err)}
	doc:=observationExecutionDocument{
		TaskID:taskID, Round:round,
		Fence:[]observationFenceIdentity{{ExecutionID:"exec-current-task",Operation:"shadow-eval",ParamsDigest:"current-digest"}},
		Records:[]ObservationExecutionRecord{{ExecutionID:"exec-current-task",TaskID:taskID,Operation:"shadow-eval",
			ParamsDigest:"current-digest",Status:ObservationExecutionStatusPass,DecisionRound:round,
			CompletedAtRFC3339:time.Now().UTC().Format(time.RFC3339Nano)}},
	}
	if err:=st.writeObservationDocument(doc);err!=nil{t.Fatal(err)}
	exists,err:=st.HasObservationExecution("shadow-eval","current-digest",round)
	if err!=nil || !exists {t.Fatalf("current task fence is lost: %t %v",exists,err)}
	// A foreign-task record is not a display-only curiosity: it is corrupt
	// task-owned state and must not be silently imported or adopted.
	doc.Records=append(doc.Records,ObservationExecutionRecord{
		ExecutionID:"exec-old-task",TaskID:"old-task-id",Operation:"shadow-eval",
		ParamsDigest:"shared-digest",Status:ObservationExecutionStatusPass,
		DecisionRound:round,CompletedAtRFC3339:time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err:=st.writeObservationDocument(doc);err==nil{t.Fatal("foreign task record was accepted")}
}
