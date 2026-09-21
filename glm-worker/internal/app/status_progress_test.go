package app

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteStatusProjectsMilestoneProgressWithoutModelCall(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	plan := fmt.Sprintf(`{"version":1,"task_id":%q,"active_task_path":"IMPLEMENTATION_TASKS/large.md","task_contract_sha256":"digest","current_index":1,"milestones":[{"id":"one","scope":"one","acceptance":"one","status":"complete","completion":{"completed_at":"2026-09-21T00:00:00Z","summary":"complete","task_contract_sha256":"digest","snapshot":{"version":1,"head":"","index_digest":"","worktree_digest":""}}},{"id":"two","scope":"two","acceptance":"two","status":"pending"},{"id":"three","scope":"three","acceptance":"three","status":"pending"}],"updated_at":"2026-09-21T00:00:00Z"}`, taskID)
	if err := st.Write(state.ExecutionMilestonesStateFile, plan); err != nil {
		t.Fatal(err)
	}
	writeTaskEventLines(t, st, taskID, state.TaskEventRecord{
		TaskID: taskID, CallID: "review-1", Role: string(state.ReviewerRole), Phase: "reviewer-1",
		ModelAlias: "haiku", Kind: "system", Timestamp: time.Now().UTC(),
	})
	before, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	after, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.ModelCalls != before.ModelCalls {
		t.Fatalf("status progress added model calls: before=%d after=%d", before.ModelCalls, after.ModelCalls)
	}
	if output.Progress == nil {
		t.Fatal("status progress is nil")
	}
	progress := output.Progress
	if progress.Status != "estimated" || progress.Band != "middle" || progress.Precision != "coarse" {
		t.Fatalf("progress = %#v", progress)
	}
	if progress.PhaseStage != "review" {
		t.Fatalf("progress phase stage = %q want review", progress.PhaseStage)
	}
	if !reflect.DeepEqual(progress.CompletedMilestones, []string{"one"}) || !reflect.DeepEqual(progress.PendingMilestones, []string{"three"}) {
		t.Fatalf("progress milestone partition = %#v", progress)
	}
	if progress.CurrentMilestone == nil || progress.CurrentMilestone.ID != "two" || progress.CurrentMilestone.Position != 2 || progress.CurrentMilestone.Count != 3 {
		t.Fatalf("current milestone = %#v", progress.CurrentMilestone)
	}
	statusString(t, "current_phase", output.CurrentPhase, "reviewer-1")
	if output.TaskElapsedMS == nil {
		t.Fatal("existing task_elapsed_ms disappeared from status")
	}
}

func TestExecuteStatusKeepsMachineJSONWhenMilestoneStateIsUnreadable(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(state.ExecutionMilestonesStateFile, `{not-json`); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	if output.Progress == nil || output.Progress.Status != "indeterminate" {
		t.Fatalf("progress = %#v", output.Progress)
	}
	if output.Progress.Reason != "milestone-state-unavailable" || output.Progress.Precision != "unavailable" {
		t.Fatalf("unreadable progress authority = %#v", output.Progress)
	}
}
