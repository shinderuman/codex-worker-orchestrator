package state

import "testing"

func TestPendingDefectRegistrationOverridesParentActionPlan(t *testing.T) {
	st := newParentActionTestStore(t)
	source := "IMPLEMENTATION_TASKS/active.md"
	target := "IMPLEMENTATION_TASKS/follow-up.md"
	if err := st.Write("active-task", source); err != nil {
		t.Fatal(err)
	}
	registration, created, err := st.RecordPendingDefectRegistration(target, source)
	if err != nil {
		t.Fatal(err)
	}
	if !created || registration.TaskPath != target || registration.SourceActiveTask != source {
		t.Fatalf("registration = %#v created=%v", registration, created)
	}
	duplicate, created, err := st.RecordPendingDefectRegistration(target, source)
	if err != nil {
		t.Fatal(err)
	}
	if created || duplicate != registration {
		t.Fatalf("duplicate = %#v created=%v want %#v", duplicate, created, registration)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionBindDefectTask || len(plan.AllowedActions) != 1 || !plan.Allows(ParentActionBindDefectTask) || plan.RequiredActionParameters["task"] != target {
		t.Fatalf("pending defect plan = %#v", plan)
	}
	for _, action := range []ParentAction{ParentActionAccept, ParentActionComplete, ParentActionFix, ParentActionPark, ParentActionResume, ParentActionDecision} {
		if _, admitted, err := st.AdmitParentAction(action); err != nil || admitted {
			t.Fatalf("%s admission = %v err=%v", action, admitted, err)
		}
	}

	bound, err := st.BindPendingDefectRegistration(target)
	if err != nil {
		t.Fatal(err)
	}
	if bound != registration {
		t.Fatalf("bound = %#v want %#v", bound, registration)
	}
	after, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if after.RequiredAction != ParentActionNone || len(after.AllowedActions) != 0 {
		t.Fatalf("post-bind plan = %#v", after)
	}
}

func TestPendingDefectRegistrationRejectsStaleActiveBinding(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/active.md"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RecordPendingDefectRegistration("IMPLEMENTATION_TASKS/follow-up.md", "IMPLEMENTATION_TASKS/active.md"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/other.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("stale defect registration was accepted")
	}
}

func TestPendingDefectRegistrationClearsOnFreshTask(t *testing.T) {
	st := newParentActionTestStore(t)
	source := "IMPLEMENTATION_TASKS/active.md"
	if err := st.Write("active-task", source); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RecordPendingDefectRegistration("IMPLEMENTATION_TASKS/follow-up.md", source); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	registrations, err := st.PendingDefectRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 0 {
		t.Fatalf("fresh task retained pending defect registrations: %#v", registrations)
	}
}
