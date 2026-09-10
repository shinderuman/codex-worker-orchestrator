package state

import "testing"

func TestAdmitParentActionUsesCanonicalPlan(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}

	plan, admitted, err := st.AdmitParentAction(ParentActionDecision)
	if err != nil {
		t.Fatal(err)
	}
	if !admitted || plan.RequiredAction != ParentActionDecision {
		t.Fatalf("decision admission = admitted %v plan %#v", admitted, plan)
	}

	_, admitted, err = st.AdmitParentAction(ParentActionFix)
	if err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("fix was admitted while a decision is required")
	}
}

func TestAdmitParentActionFailsClosedOnLifecycleInconsistency(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	_, admitted, err := st.AdmitParentAction(ParentActionDecision)
	if err == nil {
		t.Fatal("inconsistent waiting-decision state was admitted")
	}
	if admitted {
		t.Fatal("inconsistent lifecycle reported an admitted action")
	}
}

func TestAdmitNewTaskUsesCanonicalPlan(t *testing.T) {
	st := newParentActionTestStore(t)

	plan, admitted, err := st.AdmitNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if !admitted || plan.RequiredAction != ParentActionNone {
		t.Fatalf("active task admission = admitted %v plan %#v", admitted, plan)
	}

	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	plan, admitted, err = st.AdmitNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if admitted || plan.RequiredAction != ParentActionDecision {
		t.Fatalf("waiting decision new-task admission = admitted %v plan %#v", admitted, plan)
	}
}
