package app

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffProjectsDefectBindingAction(t *testing.T) {
	taskPath := "IMPLEMENTATION_TASKS/follow-up.md"
	spec, ok := parentActionSpec(string(state.ParentActionBindDefectTask), map[string]string{"task": taskPath})
	if !ok {
		t.Fatal("bind-defect-task action spec was not projected")
	}
	wantCommand := []string{"glm-parent-action", "bind-defect-task", "--task", taskPath}
	if spec.Kind != "direct" || !reflect.DeepEqual(spec.Command, wantCommand) || spec.Parameters["task"] != taskPath {
		t.Fatalf("spec = %#v want command %#v", spec, wantCommand)
	}
	if _, ok := parentActionSpec(string(state.ParentActionBindDefectTask), nil); ok {
		t.Fatal("bind-defect-task projected without machine-bound task parameter")
	}
}

func TestPendingDefectRegistrationBlocksParentRequestStopBeforeTaskPlanBinding(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	target := "IMPLEMENTATION_TASKS/follow-up.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", active); err != nil {
		t.Fatal(err)
	}
	if _, created, err := st.RecordPendingDefectRegistration(target, active); err != nil || !created {
		t.Fatalf("record pending defect registration: created=%t err=%v", created, err)
	}

	projection, err := BuildCurrentParentRequestCompletionProjection(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if projection.CompletionAdmitted || projection.StopAdmitted {
		t.Fatalf("pending defect registration admitted completion/stop: %#v", projection)
	}
	if projection.Continuation.State != projectContinuationContinueNow || projection.Continuation.Task != active || projection.Continuation.RequiredAction != string(state.ParentActionBindDefectTask) {
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
