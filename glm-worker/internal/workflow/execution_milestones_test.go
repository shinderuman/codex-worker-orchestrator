package workflow

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseExecutionTaskPlanPayloadRejectsAmbiguousMilestones(t *testing.T) {
	_, _, err := ParseExecutionTaskPlanPayload(`{"request":"work","milestones":[{"id":"same","scope":"a","acceptance":"a"},{"id":"same","scope":"b","acceptance":"b"}]}`)
	if err == nil || !strings.Contains(err.Error(), "duplicate execution milestone id") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecutionMilestonesAdvanceBeforeSingleFinalReview(t *testing.T) {
	w, st, runner, taskPath := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: implementedPacket("first complete")},
		{structured: implementedPacket("second complete")},
		{structured: passPacket()},
	})
	before, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	definitions := []ExecutionMilestoneDefinition{
		{ID: "capture", Scope: "implement capture boundary", Acceptance: "capture tests pass"},
		{ID: "index", Scope: "implement index boundary", Acceptance: "index tests pass", FreshWorker: true},
	}
	if err := w.ExecuteNewTaskWithMilestones("implement the ACTIVE task", definitions); err != nil {
		t.Fatal(err)
	}
	if want := []string{"worker-new", "worker-milestone-2", "reviewer-1"}; !reflect.DeepEqual(runner.phases, want) {
		t.Fatalf("phases = %v want %v", runner.phases, want)
	}
	if strings.Contains(runner.prompts[0], "first complete") {
		t.Fatalf("first milestone prompt unexpectedly contains completion evidence: %s", runner.prompts[0])
	}
	if !strings.Contains(runner.prompts[1], `"id":"capture"`) || !strings.Contains(runner.prompts[1], "first complete") {
		t.Fatalf("second milestone prompt does not carry durable completion evidence: %s", runner.prompts[1])
	}
	if !strings.Contains(runner.prompts[1], `"id":"index"`) {
		t.Fatalf("second milestone prompt does not identify current milestone: %s", runner.prompts[1])
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.CurrentIndex != 2 || len(plan.Milestones) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	for index, record := range plan.Milestones {
		if record.Status != executionMilestoneComplete || record.Completion == nil || record.Completion.Snapshot != fixedSnapshot {
			t.Fatalf("milestone %d = %#v", index, record)
		}
		if record.Completion.TaskContractSHA256 == "" {
			t.Fatalf("milestone %d has no task contract hash", index)
		}
	}
	after, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("ACTIVE task contract changed:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestExecutionMilestoneAuthorityFailsClosedOnTaskContractChange(t *testing.T) {
	w, st, _, taskPath := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	relative := "IMPLEMENTATION_TASKS/large.md"
	if err := st.Write(activeTaskStateKey, relative); err != nil {
		t.Fatal(err)
	}
	definitions := []ExecutionMilestoneDefinition{
		{ID: "one", Scope: "one", Acceptance: "one"},
		{ID: "two", Scope: "two", Acceptance: "two"},
	}
	if err := w.initializeExecutionMilestones(definitions, relative); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(taskPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nsemantic amendment\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	active, err := w.hasPendingExecutionMilestone()
	if !active || err == nil || !strings.Contains(err.Error(), "task contract changed") {
		t.Fatalf("active=%v err=%v", active, err)
	}
}

func TestReviseExecutionMilestonesBindsStoppedResumeCheckpoint(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	relative := "IMPLEMENTATION_TASKS/large.md"
	if err := st.Write(activeTaskStateKey, relative); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: "opus", Prompt: "prompt", OriginalPrompt: "prompt", Request: "request",
		StopKind: state.ResumeStopRateLimited, ReportOnly: false,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	definitions := []ExecutionMilestoneDefinition{
		{ID: "remaining-a", Scope: "finish a", Acceptance: "a complete"},
		{ID: "remaining-b", Scope: "finish b", Acceptance: "b complete"},
	}
	result, err := ReviseExecutionMilestones(w.config, st, definitions, testFixedTime)
	if err != nil {
		t.Fatal(err)
	}
	if result.CurrentID != "remaining-a" || result.CurrentIndex != 0 {
		t.Fatalf("revision = %+v", result)
	}
	stored, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if stored.ExecutionMilestoneID != "remaining-a" {
		t.Fatalf("checkpoint milestone = %q", stored.ExecutionMilestoneID)
	}
}

func newExecutionMilestoneWorkflow(t *testing.T, steps []runnerStep) (*Workflow, *state.StateStore, *scriptedRunner, string) {
	t.Helper()
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "milestones@example.invalid")
	gitScope(t, repo, "config", "user.name", "milestones-test")
	taskRelative := "IMPLEMENTATION_TASKS/large.md"
	taskPath := filepath.Join(repo, filepath.FromSlash(taskRelative))
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	task := `# Large task

## Original instruction

implement

## Amendments

none

## External feasibility

status: not-applicable

## Purpose

exercise milestones

## Contract

- keep one semantic task

## Must not

- do not create a second lifecycle

## Acceptance criteria

- final review covers the task
`
	if err := os.WriteFile(taskPath, []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlanWithActive(t, repo, "## ACTIVE\n\n- `"+taskRelative+"`\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	cfg := config.AppConfig{
		WorkerModel: "opus", ReviewerModel: "haiku", HighRiskReviewerModel: "sonnet",
		RoutineEffort: "high", EscalatedEffort: "high", MaxAutoFixRounds: 2,
		TelemetryContent: true, RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "milestones",
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedRunner{steps: steps}
	w := NewWorkflow(cfg, st, runner, io.Discard)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }
	w.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {
		snapshot, err := w.captureSnapshot(repoRoot)
		if err != nil {
			return snapshot, err
		}
		parents, err := state.CaptureParentFileStates(repoRoot)
		if err != nil {
			return snapshot, err
		}
		snapshot.ParentFiles = &parents
		return snapshot, nil
	}
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	clock := newFakeClock()
	w.now = clock.nowFunc
	w.sleep = clock.sleepFunc
	w.jitter = identityJitter
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	w.captureQualitySurface = func(string) (string, error) { return "quality-baseline", nil }
	return w, st, runner, taskPath
}
