package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestAwaitObservationNoGoDefersCompletionWithoutAnotherDispatch(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: string(WorkerRole), Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	admission, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionDecision || !plan.Allows(ParentActionDecision) || plan.Allows(ParentActionNoGo) {
		t.Fatalf("generic decision plan = %#v", plan)
	}
	awaited, err := st.AwaitObservationNoGo(admission)
	if err != nil || !awaited {
		t.Fatalf("await no-go = %v err=%v", awaited, err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion || st.Exists("pending-decision") {
		t.Fatalf("awaiting state = status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if st.OpenParentReviewLabel() != "none" {
		t.Fatalf("parent review remains open: %s", st.OpenParentReviewLabel())
	}
	outcome, err := st.CurrentParentCompletionOutcome()
	if err != nil || outcome == nil || outcome.Terminal != SessionRotationTerminalNoGo || outcome.Risk != string(packet.RiskHigh) {
		t.Fatalf("canonical no-go outcome = %#v err=%v", outcome, err)
	}
	plan, err = st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionComplete || plan.Allows(ParentActionNoGo) || plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("awaiting plan = %#v", plan)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeNoGo] != 1 || stats.ModelCalls != 0 ||
		stats.CompletionTerminal != SessionRotationTerminalNoGo || stats.AcceptedRisk != string(packet.RiskHigh) {
		t.Fatalf("awaiting stats = outcomes:%v model_calls:%d terminal:%s risk:%s", stats.ParentOutcomes, stats.ModelCalls, stats.CompletionTerminal, stats.AcceptedRisk)
	}
	logs, err := st.ReadModelCallLogs(stats.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range logs {
		if record.CallType == CallTypeEvent && record.Phase == ParentPhaseClose && record.Outcome == ParentOutcomeNoGo {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("terminal no-go event missing: %#v", logs)
	}

	completed, err := st.CompleteParentAwaiting(nil)
	if err != nil || !completed {
		t.Fatalf("awaiting completion = %v err=%v", completed, err)
	}
	if st.TaskStatus() != TaskStatusComplete {
		t.Fatalf("completion status = %s", st.TaskStatus())
	}
	plan, err = st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionNone || len(plan.AllowedActions) != 0 {
		t.Fatalf("completed plan = %#v", plan)
	}
}

func TestAwaitObservationNoGoRejectsStaleLifecycleAdmission(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	admission, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatal(err)
	}
	admission.Round++
	if awaited, err := st.AwaitObservationNoGo(admission); err == nil || awaited {
		t.Fatalf("stale no-go = %v err=%v", awaited, err)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("stale admission mutated state: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}
