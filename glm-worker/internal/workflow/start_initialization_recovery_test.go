package workflow

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const unstartedMilestoneRequest = "start the successor task"

func crashUnstartedMilestoneStartup(t *testing.T) (config.AppConfig, *controller.Store, *state.StateStore, *executionmilestone.Plan) {
	t.Helper()
	_, cfg, store, st, _, _ := newQualityMutationFixture(t)
	cfg.RepoSearch = true
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	crashed := NewWorkflow(cfg, st, &scriptedRunner{}, io.Discard)
	crashed.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		panic("wrapper process died during startup navigation")
	}
	definitions := []executionunit.MilestoneDefinition{
		{ID: "first", Scope: "implement first boundary", Acceptance: "first boundary verified"},
		{ID: "second", Scope: "implement second boundary", Acceptance: "second boundary verified"},
	}
	func() {
		defer func() { _ = recover() }()
		if err := crashed.ExecuteNewTaskWithMilestones(unstartedMilestoneRequest, definitions); err != nil {
			t.Fatal(err)
		}
	}()
	plan, err := executionmilestone.Load(st)
	if err != nil || plan == nil || plan.CurrentIndex != 0 || len(plan.Milestones) != 2 {
		t.Fatalf("crashed startup left no unstarted milestone plan: %#v %v", plan, err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("crashed startup status = %q", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("crashed startup left a resume checkpoint: %v", err)
	}
	return cfg, store, st, plan
}

func recoveryWorkflow(cfg config.AppConfig, st *state.StateStore) *Workflow {
	w := NewWorkflow(cfg, st, &scriptedRunner{}, io.Discard)
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		return reposearch.Report{}, nil
	}
	return w
}

func TestRecoverUnstartedMilestoneRebindsCrashedStartupIntoLegalStop(t *testing.T) {
	cfg, _, st, plan := crashUnstartedMilestoneStartup(t)
	if err := recoveryWorkflow(cfg, st).RecoverUnstartedMilestone(); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.StopKind != state.ResumeStopInterrupted || checkpoint.Phase != "worker-new" || checkpoint.ExecutionMilestoneID != plan.Milestones[0].ID || checkpoint.Request != unstartedMilestoneRequest {
		t.Fatalf("recovered startup checkpoint = %#v", checkpoint)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("recovered startup status = %q", st.TaskStatus())
	}
	preserved, err := executionmilestone.Load(st)
	if err != nil || preserved.CurrentIndex != 0 || preserved.Milestones[0].Status != executionmilestone.StatusPending {
		t.Fatalf("startup recovery mutated the milestone plan: %#v %v", preserved, err)
	}
	if err := recoveryWorkflow(cfg, st).RecoverUnstartedMilestone(); err == nil {
		t.Fatal("startup recovery replaced a stopped runtime")
	}
}

func TestRecoverUnstartedMilestoneRejectsNonMilestoneRuntime(t *testing.T) {
	_, cfg, _, st, _, _ := newQualityMutationFixture(t)
	cfg.RepoSearch = true
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	crashed := NewWorkflow(cfg, st, &scriptedRunner{}, io.Discard)
	crashed.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		panic("wrapper process died during startup navigation")
	}
	func() {
		defer func() { _ = recover() }()
		if err := crashed.ExecuteNewTask("start a plain task"); err != nil {
			t.Fatal(err)
		}
	}()
	if plan, err := executionmilestone.Load(st); err != nil || plan != nil {
		t.Fatalf("crashed plain startup left a milestone plan: %#v %v", plan, err)
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("crashed plain startup left a resume checkpoint: %v", err)
	}
	if err := recoveryWorkflow(cfg, st).RecoverUnstartedMilestone(); err == nil {
		t.Fatal("startup recovery converted a non-milestone runtime")
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("rejected startup recovery left a checkpoint: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("rejected startup recovery changed the runtime status: %q", st.TaskStatus())
	}
}

func TestRecoverUnstartedMilestoneRejectsExistingModelExecution(t *testing.T) {
	cfg, store, st, _ := crashUnstartedMilestoneStartup(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "crashed-startup-attempt",
		CallType: state.CallTypeTask,
		TaskID:   taskID,
		Phase:    "worker-new",
		Outcome:  "error",
	})
	if err := recoveryWorkflow(cfg, st).RecoverUnstartedMilestone(); err == nil {
		t.Fatal("startup recovery replaced an existing model execution")
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("rejected startup recovery left a checkpoint: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("rejected startup recovery changed the runtime status: %q", st.TaskStatus())
	}
	after, err := store.LoadHead()
	if err != nil || after.ControllerGeneration != before.ControllerGeneration || after.LiveLeaseID != before.LiveLeaseID {
		t.Fatalf("rejected startup recovery mutated controller authority: %#v %v", after, err)
	}
}
