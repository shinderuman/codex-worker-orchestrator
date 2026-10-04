package app

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffMapsDefectBindingToControllerSemanticAction(t *testing.T) {
	action := canonicalParentAction(string(state.ParentActionBindDefectTask))
	if action != "controller-semantic" {
		t.Fatalf("canonical defect action = %q", action)
	}
	spec, ok := parentActionSpec(action, nil)
	if !ok || spec.Kind != "staged" || !reflect.DeepEqual(spec.PrepareCommand, []string{"glm-parent-action", "prepare", "controller-semantic"}) {
		t.Fatalf("controller semantic spec = %#v ok=%t", spec, ok)
	}
}

func TestPendingDefectRegistrationBlocksParentRequestStopBeforeTaskPlanBinding(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	target := "IMPLEMENTATION_TASKS/follow-up.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st := startActivatedParentHandoffTask(t, cfg)
	if err := st.Write("active-task", active); err != nil {
		t.Fatal(err)
	}
	if _, created, err := st.RecordPendingDefectRegistration(target, active); err != nil || !created {
		t.Fatalf("record pending defect registration: created=%t err=%v", created, err)
	}

	gate := parentcontinuation.Build(cfg, st)
	if !gate.Consistent || gate.ParentRequest == nil {
		t.Fatalf("focused continuation projection = %#v", gate)
	}
	projection := gate.ParentRequest
	if projection.CompletionAdmitted || projection.StopAdmitted {
		t.Fatalf("pending defect registration admitted completion/stop: %#v", projection)
	}
	if projection.Continuation.State != repositoryproject.ContinuationContinueNow || projection.Continuation.Task != active || projection.Continuation.RequiredAction != string(state.ParentActionBindDefectTask) {
		t.Fatalf("pending defect continuation = %#v", projection.Continuation)
	}
}

func TestPendingDefectBindingSuppressesMilestoneSideAction(t *testing.T) {
	status := string(state.TaskStatusWaitingSolReview)
	required := string(state.ParentActionBindDefectTask)
	actions := []string{string(state.ParentActionBindDefectTask)}
	projected := withExecutionMilestoneReconsideration(&status, &required, false, actions)
	if !reflect.DeepEqual(projected, actions) {
		t.Fatalf("projected actions = %#v want %#v", projected, actions)
	}
}
