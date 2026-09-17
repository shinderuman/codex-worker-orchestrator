package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentContinuationFatalActiveWithoutActionIsInconsistent(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "fatal-active",
		CallType: state.CallTypeTask,
		TaskID:   taskID,
		Phase:    "worker-new",
		Outcome:  "error",
	})
	active := string(state.TaskStatusActive)
	none := string(state.ParentActionNone)
	output := parentHandoffOutput{
		Consistent:     true,
		TaskStatus:     &active,
		RequiredAction: &none,
		AllowedActions: []string{},
		ParentRequest: &ParentRequestCompletionProjection{
			Continuation: ProjectContinuation{State: projectContinuationContinueNow},
		},
	}

	validateParentContinuationActionability(st, &output)
	if output.Consistent || output.Inconsistency == nil {
		t.Fatalf("fatal active handoff remained consistent: %#v", output)
	}
	if output.RequiredAction != nil || len(output.AllowedActions) != 0 {
		t.Fatalf("fatal active handoff retained actions: %#v", output)
	}
	recovery := projectParentHandoffRecovery(output)
	if recovery.Consistent {
		t.Fatalf("fatal active recovery projection remained consistent: %#v", recovery)
	}
}

func TestParentContinuationHealthyActiveWithoutActionRemainsConsistent(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "healthy-active",
		CallType: state.CallTypeTask,
		TaskID:   taskID,
		Phase:    "worker-new",
		Outcome:  "success",
	})
	active := string(state.TaskStatusActive)
	none := string(state.ParentActionNone)
	output := parentHandoffOutput{
		Consistent:     true,
		TaskStatus:     &active,
		RequiredAction: &none,
		AllowedActions: []string{},
		ParentRequest: &ParentRequestCompletionProjection{
			Continuation: ProjectContinuation{State: projectContinuationContinueNow},
		},
	}

	validateParentContinuationActionability(st, &output)
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != none {
		t.Fatalf("healthy active handoff changed: %#v", output)
	}
}
