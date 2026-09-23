package workflow

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestProjectExecutionProgressIsIndeterminateWithoutMilestones(t *testing.T) {
	_, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	got := ProjectExecutionProgress(st, "worker-new", string(state.WorkerRole))
	if got.Status != "indeterminate" || got.Precision != "unavailable" || got.Band != "" {
		t.Fatalf("progress = %#v", got)
	}
	if got.Basis != "single-or-untracked-execution-unit" || got.Reason != "no-execution-milestones" {
		t.Fatalf("indeterminate basis = %#v", got)
	}
	if got.PhaseStage != "implementation" {
		t.Fatalf("phase stage = %q want implementation", got.PhaseStage)
	}
}

func TestProjectExecutionProgressUsesMilestonePositionAndPhaseBand(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/large.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	definitions := []executionunit.MilestoneDefinition{
		{ID: "one", Scope: "one", Acceptance: "one"},
		{ID: "two", Scope: "two", Acceptance: "two"},
		{ID: "three", Scope: "three", Acceptance: "three"},
	}
	if err := w.initializeExecutionMilestones(definitions, taskPath); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name         string
		currentIndex int
		phase        string
		role         string
		wantBand     string
		wantStage    string
		completed    []string
		current      string
		pending      []string
	}{
		{
			name: "first implementation", currentIndex: 0,
			phase: "worker-new", role: string(state.WorkerRole), wantBand: "early", wantStage: "implementation",
			current: "one", pending: []string{"two", "three"},
		},
		{
			name: "first review", currentIndex: 0,
			phase: "reviewer-1", role: string(state.ReviewerRole), wantBand: "early-to-middle", wantStage: "review",
			current: "one", pending: []string{"two", "three"},
		},
		{
			name: "middle", currentIndex: 1,
			phase: "worker-milestone-2", role: string(state.WorkerRole), wantBand: "middle", wantStage: "implementation",
			completed: []string{"one"}, current: "two", pending: []string{"three"},
		},
		{
			name: "final implementation", currentIndex: 2,
			phase: "worker-explicit-fix-2", role: string(state.WorkerRole), wantBand: "middle-to-late", wantStage: "implementation",
			completed: []string{"one", "two"}, current: "three",
		},
		{
			name: "final review", currentIndex: 2,
			phase: "reviewer-1", role: string(state.ReviewerRole), wantBand: "late", wantStage: "review",
			completed: []string{"one", "two"}, current: "three",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setExecutionProgressIndex(t, st, tc.currentIndex)

			got := ProjectExecutionProgress(st, tc.phase, tc.role)
			if got.Status != "estimated" || got.Precision != "coarse" || got.Basis != "execution-milestones+phase" {
				t.Fatalf("progress authority = %#v", got)
			}
			if got.Band != tc.wantBand || got.PhaseStage != tc.wantStage {
				t.Fatalf("band/stage = %q/%q want %q/%q", got.Band, got.PhaseStage, tc.wantBand, tc.wantStage)
			}
			if !reflect.DeepEqual(got.CompletedMilestones, tc.completed) {
				t.Fatalf("completed = %#v want %#v", got.CompletedMilestones, tc.completed)
			}
			if got.CurrentMilestone == nil || got.CurrentMilestone.ID != tc.current || got.CurrentMilestone.Position != tc.currentIndex+1 || got.CurrentMilestone.Count != len(definitions) {
				t.Fatalf("current = %#v want id=%q position=%d count=%d", got.CurrentMilestone, tc.current, tc.currentIndex+1, len(definitions))
			}
			if !reflect.DeepEqual(got.PendingMilestones, tc.pending) {
				t.Fatalf("pending = %#v want %#v", got.PendingMilestones, tc.pending)
			}
		})
	}
}

func TestProjectExecutionProgressFailsClosedOnStaleMilestoneTask(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/large.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	if err := w.initializeExecutionMilestones([]executionunit.MilestoneDefinition{
		{ID: "one", Scope: "one", Acceptance: "one"},
		{ID: "two", Scope: "two", Acceptance: "two"},
	}, taskPath); err != nil {
		t.Fatal(err)
	}
	plan, err := executionmilestone.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	plan.TaskID = "stale-task"
	if err := executionmilestone.Save(st, plan); err != nil {
		t.Fatal(err)
	}

	got := ProjectExecutionProgress(st, "worker-new", string(state.WorkerRole))
	if got.Status != "indeterminate" || got.Reason != "milestone-task-mismatch" || got.Basis != "machine-state" {
		t.Fatalf("stale progress = %#v", got)
	}
}

func TestProjectExecutionProgressMarksCompletedMilestonePlanExactlyComplete(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/large.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	definitions := []executionunit.MilestoneDefinition{
		{ID: "one", Scope: "one", Acceptance: "one"},
		{ID: "two", Scope: "two", Acceptance: "two"},
	}
	if err := w.initializeExecutionMilestones(definitions, taskPath); err != nil {
		t.Fatal(err)
	}
	setExecutionProgressIndex(t, st, len(definitions))
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}

	got := ProjectExecutionProgress(st, "reviewer-1", string(state.ReviewerRole))
	if got.Status != "complete" || got.Band != "complete" || got.Precision != "exact" {
		t.Fatalf("complete progress = %#v", got)
	}
	if got.CurrentMilestone != nil || len(got.PendingMilestones) != 0 {
		t.Fatalf("complete milestone projection has remaining work: %#v", got)
	}
	if !reflect.DeepEqual(got.CompletedMilestones, []string{"one", "two"}) {
		t.Fatalf("completed milestones = %#v", got.CompletedMilestones)
	}
}

func setExecutionProgressIndex(t *testing.T, st *state.StateStore, currentIndex int) {
	t.Helper()
	plan, err := executionmilestone.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	plan.CurrentIndex = currentIndex
	for index := range plan.Milestones {
		milestone := &plan.Milestones[index]
		if index < currentIndex {
			milestone.Status = executionmilestone.StatusComplete
			milestone.Completion = &executionmilestone.Completion{
				CompletedAt:        testFixedTime,
				Summary:            "complete",
				TaskContractSHA256: plan.TaskContractSHA256,
				Snapshot:           fixedSnapshot,
			}
		} else {
			milestone.Status = executionmilestone.StatusPending
			milestone.Completion = nil
		}
	}
	if err := executionmilestone.Save(st, plan); err != nil {
		t.Fatal(err)
	}
}
