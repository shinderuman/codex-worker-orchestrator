package state

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func observationFenceClaim(t *testing.T, st *StateStore, executionID, digest string, round int) ObservationExecutionRecord {
	t.Helper()
	taskID,err:=st.TaskID()
	if err!=nil {t.Fatal(err)}
	return ObservationExecutionRecord{
		ExecutionID:executionID,TaskID:taskID,Operation:"shadow-eval",ParamsDigest:digest,
		Status:ObservationExecutionStatusInFlight,DecisionRound:round,
		DeadlineMS:30000,StartedAtRFC3339:time.Now().UTC().Format(time.RFC3339Nano),
	}
}
func finishFenceClaim(t *testing.T,st *StateStore,rec ObservationExecutionRecord) {
	t.Helper()
	rec.Status=ObservationExecutionStatusPass
	rec.CompletedAtRFC3339=time.Now().UTC().Format(time.RFC3339Nano)
	if err:=st.CompleteObservationExecution(rec);err!=nil {t.Fatal(err)}
}

func TestObservationReplayFenceCapacityAndRecovery(t *testing.T) {
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	round,err:=st.ObservationExecutionRound();if err!=nil{t.Fatal(err)}
	for i:=0;i<observationReplayFenceLimit;i++ {
		rec:=observationFenceClaim(t,st,fmt.Sprintf("capacity-%04d",i),fmt.Sprintf("param-%04d",i),round)
		if err:=st.BeginObservationExecution(rec);err!=nil{t.Fatalf("claim %d: %v",i,err)}
		if i!=0 {finishFenceClaim(t,st,rec)}
	}
	replay:=observationFenceClaim(t,st,"new-id","param-0001",round)
	if err:=st.BeginObservationExecution(replay);err==nil || !strings.Contains(err.Error(),"再実行"){
		t.Fatalf("duplicate must be rejected even at capacity: %v",err)
	}
	extra:=observationFenceClaim(t,st,"capacity-1025","param-new",round)
	if err:=st.BeginObservationExecution(extra);err==nil || !strings.Contains(err.Error(),"capacity exceeded"){
		t.Fatalf("1025th identity admitted: %v",err)
	}
	if err:=st.ResolveObservationExecutionIndeterminate("capacity-0000","interrupted",time.Now().UTC());err!=nil{
		t.Fatalf("recovery at capacity: %v",err)
	}
	exists,err:=st.HasObservationExecution("shadow-eval","param-0000",round)
	if err!=nil || !exists{t.Fatalf("recovered identity was lost: %t %v",exists,err)}
	if err:=st.BeginObservationExecution(extra);err==nil{t.Fatal("recovery incorrectly freed capacity")}
}

func TestObservationReplayFenceRoundTransitionRequiresResolvedClaim(t *testing.T) {
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	admitObservationDecisionBoundary(t,st)
	first,err:=st.ObservationExecuteAdmission();if err!=nil{t.Fatal(err)}
	old:=observationFenceClaim(t,st,"old-inflight","shared",first.Round)
	if err:=st.BeginObservationExecution(old);err!=nil{t.Fatal(err)}
	if _,err:=st.BeginParentDecision();err!=nil{t.Fatal(err)}
	if err:=st.CommitParentActionBegin();err!=nil{t.Fatal(err)}
	admitObservationDecisionBoundary(t,st)
	next,err:=st.ObservationExecuteAdmission();if err!=nil{t.Fatal(err)}
	fresh:=observationFenceClaim(t,st,"next-round","shared",next.Round)
	if err:=st.BeginObservationExecution(fresh);err==nil{t.Fatal("new round admitted while old in-flight remains")}
	if err:=st.CompleteObservationExecution(oldToComplete(old));err==nil{
		t.Fatal("late completion from old canonical round was adopted")
	}
	if err:=st.ResolveObservationExecutionIndeterminate(old.ExecutionID,"boundary changed",time.Now().UTC());err!=nil{t.Fatal(err)}
	if err:=st.BeginObservationExecution(fresh);err!=nil{t.Fatalf("new round after durable recovery: %v",err)}
	if err:=st.BeginObservationExecution(old);err==nil{t.Fatal("old round was admitted after GC")}
	doc,err:=st.readObservationDocument();if err!=nil{t.Fatal(err)}
	if doc.Round!=next.Round || len(doc.Fence)!=1 {t.Fatalf("new-round fence=%+v",doc)}
	if err:=st.CompleteObservationExecution(oldToComplete(old));err==nil{t.Fatal("old completion adopted after fence GC")}
}
func oldToComplete(r ObservationExecutionRecord)ObservationExecutionRecord {
	r.Status=ObservationExecutionStatusPass
	r.CompletedAtRFC3339=time.Now().UTC().Format(time.RFC3339Nano)
	return r
}

func TestObservationReplayFenceMissingOldAndCorruptStateFailClosed(t *testing.T){
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	round,err:=st.ObservationExecutionRound();if err!=nil{t.Fatal(err)}
	claim:=observationFenceClaim(t,st,"persisted","params",round)
	if err:=st.BeginObservationExecution(claim);err!=nil{t.Fatal(err)}
	other:=&StateStore{dir:st.dir}
	if exists,err:=other.HasObservationExecution(claim.Operation,claim.ParamsDigest,round);err!=nil || !exists{
		t.Fatalf("restart discarded durable fence: %t %v",exists,err)
	}
	for _,input:=range []string{
		"[]", "{}", "{not-json",
		`{"task_id":"different","round":1,"fence":[],"records":[]}`,
		`{"task_id":"x","round":1,"fence":[],"records":null}`,
	}{
		if err:=st.Write(observationExecutionsStateFile,input);err!=nil{t.Fatal(err)}
		if _,err:=st.HasObservationExecution("shadow-eval","params",round);err==nil{
			t.Fatalf("accepted corrupt/old state: %s",input)
		}
	}
	if err:=st.Remove(observationExecutionsStateFile);err!=nil{t.Fatal(err)}
	if _,err:=st.HasObservationExecution("shadow-eval","params",round);err==nil{
		t.Fatal("missing state interpreted as empty fence")
	}
}

func TestObservationReplayFenceAtomicWriteFailureLeavesNoReservation(t *testing.T){
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	round,err:=st.ObservationExecutionRound();if err!=nil{t.Fatal(err)}
	rec:=observationFenceClaim(t,st,"failed-write","params",round)
	before,err:=os.ReadFile(st.Path(observationExecutionsStateFile));if err!=nil{t.Fatal(err)}
	failWritesFor(t,st,observationExecutionsStateFile)
	if err:=st.BeginObservationExecution(rec);err==nil{t.Fatal("Begin accepted failed reservation")}
	after,err:=os.ReadFile(st.Path(observationExecutionsStateFile));if err!=nil{t.Fatal(err)}
	if string(before)!=string(after){t.Fatal("failed atomic write mutated the state")}
}

func TestObservationReplayFenceRejectsDetachedVisibleIdentity(t *testing.T){
	st:=newObservationExecutionStore(t,observationExecutionDeclaration)
	round,err:=st.ObservationExecutionRound();if err!=nil{t.Fatal(err)}
	rec:=observationFenceClaim(t,st,"missing-fence","params",round)
	if err:=st.BeginObservationExecution(rec);err!=nil{t.Fatal(err)}
	doc,err:=st.readObservationDocument();if err!=nil{t.Fatal(err)}
	doc.Fence=[]observationFenceIdentity{}
	data,err:=json.Marshal(doc);if err!=nil{t.Fatal(err)}
	if err:=st.Write(observationExecutionsStateFile,string(data));err!=nil{t.Fatal(err)}
	if _,err:=st.ObservationExecutions();err==nil{t.Fatal("unfenced in-flight record was accepted")}
}
