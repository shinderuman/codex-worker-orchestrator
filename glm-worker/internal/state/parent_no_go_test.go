package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestCompleteObservationNoGoTerminatesWithoutAnotherDispatch(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: string(WorkerRole), Model: "opus"})

	if !st.ObservationNoGoEligible() {
		t.Fatal("observation decision should admit terminal no-go")
	}
	completed, err := st.CompleteObservationNoGo()
	if err != nil || !completed {
		t.Fatalf("complete no-go = %v err=%v", completed, err)
	}
	if st.TaskStatus() != TaskStatusComplete || st.Exists("pending-decision") {
		t.Fatalf("terminal state = status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if st.OpenParentReviewLabel() != "none" {
		t.Fatalf("parent review remains open: %s", st.OpenParentReviewLabel())
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionNone || len(plan.AllowedActions) != 0 {
		t.Fatalf("terminal plan = %#v", plan)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeNoGo] != 1 || stats.ModelCalls != 0 {
		t.Fatalf("terminal stats = outcomes:%v model_calls:%d", stats.ParentOutcomes, stats.ModelCalls)
	}
	logs, err := st.ReadModelCallLogs(stats.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].CallType != CallTypeEvent || logs[0].Phase != ParentPhaseClose || logs[0].Outcome != ParentOutcomeNoGo {
		t.Fatalf("terminal log = %#v", logs)
	}
}

func TestCompleteObservationNoGoRejectsGenericDecision(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/normal.md", []byte("# normal\n\n## External feasibility\n\nstatus: not-applicable\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{})

	if st.ObservationNoGoEligible() {
		t.Fatal("generic decision must not admit terminal no-go")
	}
	if completed, err := st.CompleteObservationNoGo(); err == nil || completed {
		t.Fatalf("generic no-go = %v err=%v", completed, err)
	}
	if st.TaskStatus() != TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("generic decision was mutated: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}
