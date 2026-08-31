package workflow

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionMilestonesAdvanceInsideCanonicalWorkerLifecycle(t *testing.T) {
	root := newExecutionMilestoneRepo(t, executionMilestoneTask(`{
  "milestones": [
    {"id":"capture","scope":"capture bounded evidence","acceptance":"capture is validated"},
    {"id":"integrate","scope":"integrate captured evidence","acceptance":"task-wide integration is validated","fresh_worker":true}
  ]
}`))
	cfg := config.AppConfig{
		RepoRoot:              root,
		RepoHash:              "execution-milestones",
		StateBase:             t.TempDir(),
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		RoutineEffort:         "high",
		EscalatedEffort:       "max",
		MaxAutoFixRounds:      2,
		TelemetryContent:      true,
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/task.md"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-head", runGitTest(t, root, "rev-parse", "HEAD")); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("worker.id", "worker-session-1"); err != nil {
		t.Fatal(err)
	}

	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("capture complete")},
		{structured: implementedPacket("integration complete")},
		{structured: passPacket()},
	}}
	r.onRun = func() {
		phase := r.phases[len(r.phases)-1]
		if !strings.HasPrefix(phase, "worker") || st.Exists("worker.id") {
			return
		}
		if err := st.Write("worker.id", "worker-session-2"); err != nil {
			t.Fatal(err)
		}
	}

	w := NewWorkflow(cfg, st, r, io.Discard)
	w.temp = t.TempDir()
	w.captureQualitySurface = func(string) (string, error) { return "quality-baseline", nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := w.captureQualitySurfaceBaseline(); err != nil {
		t.Fatal(err)
	}

	checkpoint := state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          cfg.WorkerModel,
		Effort:         cfg.RoutineEffort,
		Prompt:         newTaskPrompt("implement active task", "IMPLEMENTATION_TASKS/task.md"),
		OriginalPrompt: newTaskPrompt("implement active task", "IMPLEMENTATION_TASKS/task.md"),
		Request:        "implement active task",
	}
	if err := w.executeWorkerCheckpoint("implement active task", checkpoint, false); err != nil {
		t.Fatal(err)
	}

	workerCalls := 0
	reviewerCalls := 0
	for _, phase := range r.phases {
		switch {
		case strings.HasPrefix(phase, "worker"):
			workerCalls++
		case strings.HasPrefix(phase, "reviewer"):
			reviewerCalls++
		}
	}
	if workerCalls != 2 || reviewerCalls != 1 {
		t.Fatalf("phases = %v; worker=%d reviewer=%d", r.phases, workerCalls, reviewerCalls)
	}
	if !strings.Contains(r.prompts[0], "ID: capture") || !strings.Contains(r.prompts[1], "ID: integrate") {
		t.Fatalf("milestone prompts missing: %#v", r.prompts[:2])
	}
	if !strings.Contains(r.prompts[1], "capture: capture complete") || !strings.Contains(r.prompts[1], "do not reimplement completed milestones") {
		t.Fatalf("fresh continuation lost completed evidence: %s", r.prompts[1])
	}

	plan, err := w.loadExecutionMilestonePlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentIndex != 2 || len(plan.Milestones) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Milestones[0].Status != "complete" || plan.Milestones[1].Status != "complete" {
		t.Fatalf("milestone status = %#v", plan.Milestones)
	}
	if plan.Milestones[0].CompletedWorkerSessionID != "worker-session-1" || plan.Milestones[1].CompletedWorkerSessionID != "worker-session-2" {
		t.Fatalf("worker sessions = %#v", plan.Milestones)
	}
	if plan.Milestones[0].Snapshot == nil || plan.Milestones[1].Snapshot == nil {
		t.Fatalf("completion snapshots missing: %#v", plan.Milestones)
	}
	if plan.TaskContractAuthority != executionMilestoneTaskAuthority {
		t.Fatalf("task authority = %q", plan.TaskContractAuthority)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("task status = %s", st.TaskStatus())
	}
}

func TestExecutionMilestonesRejectCompletedMilestoneRevision(t *testing.T) {
	now := testFixedTime
	completed := now
	plan := &executionMilestonePlan{
		Version:               executionMilestoneVersion,
		TaskID:                "task",
		ActiveTaskPath:        "IMPLEMENTATION_TASKS/task.md",
		TaskContractAuthority: executionMilestoneTaskAuthority,
		DefinitionSHA256:      "old",
		CurrentIndex:          1,
		Milestones: []executionMilestoneRecord{
			{ID: "one", Scope: "scope one", Acceptance: "accept one", Status: "complete", CompletedAt: &completed, Snapshot: &fixedSnapshot},
			{ID: "two", Scope: "scope two", Acceptance: "accept two", Status: "pending"},
		},
		UpdatedAt: now,
	}
	definitions := []executionMilestoneDefinition{
		{ID: "one", Scope: "changed completed scope", Acceptance: "accept one"},
		{ID: "two", Scope: "scope two", Acceptance: "accept two"},
	}
	if err := reconcileExecutionMilestoneDefinitions(plan, definitions, "new", now); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("completed milestone revision error = %v", err)
	}
}

func TestExecutionMilestonesMalformedDefinitionStopsBeforeModelCall(t *testing.T) {
	root := newExecutionMilestoneRepo(t, executionMilestoneTask(`{"milestones":[{"id":"only","scope":"x","acceptance":"y"}]}`))
	cfg := config.AppConfig{RepoRoot: root, RepoHash: "bad-milestones", StateBase: t.TempDir(), WorkerModel: "opus", RoutineEffort: "high"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/task.md"); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{}
	w := NewWorkflow(cfg, st, r, io.Discard)
	w.temp = t.TempDir()
	_, err = w.runWorkerModelWithRuleActivation(state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: "opus", Effort: "high", Prompt: "base", OriginalPrompt: "base", Request: "request",
	})
	if err == nil || !strings.Contains(err.Error(), "at least two milestones") {
		t.Fatalf("error = %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("model calls = %d", len(r.prompts))
	}
}

func TestExecutionMilestonesSmallTaskLeavesPromptAndCallsUnchanged(t *testing.T) {
	root := newExecutionMilestoneRepo(t, strings.ReplaceAll(executionMilestoneTask(""), "## Execution milestones\n\n", ""))
	cfg := config.AppConfig{RepoRoot: root, RepoHash: "small-task", StateBase: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/task.md"); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, io.Discard)
	checkpoint := state.ResumeCheckpoint{Role: state.WorkerRole, Prompt: "base", OriginalPrompt: "base"}
	got, err := w.decorateExecutionMilestoneCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "base" || got.OriginalPrompt != "base" {
		t.Fatalf("small task prompt changed: %#v", got)
	}
	if st.Exists(executionMilestoneStateFile) {
		t.Fatal("small task created milestone runtime state")
	}
}

func newExecutionMilestoneRepo(t *testing.T, task string) string {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "milestone@example.invalid")
	runGitTest(t, root, "config", "user.name", "execution milestone")
	if err := os.MkdirAll(filepath.Join(root, "IMPLEMENTATION_TASKS"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeGitTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/task.md`\n")
	writeGitTestFile(t, root, "IMPLEMENTATION_RULES.md", "# rules\n")
	writeGitTestFile(t, root, "IMPLEMENTATION_HISTORY.md", "# history\n")
	writeGitTestFile(t, root, "IMPLEMENTATION_TASKS/task.md", task)
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "baseline")
	return root
}

func executionMilestoneTask(milestones string) string {
	section := ""
	if milestones != "" {
		section = "## Execution milestones\n\n```json\n" + milestones + "\n```\n\n"
	}
	return `# Task: milestone test

## Original instruction

implement active task

## Amendments

none

## Resolved references

none

## External feasibility

status: not-applicable

## Purpose

exercise durable execution milestones.

## Contract

- preserve one semantic task.

## Must not

- do not weaken task-wide review.

## Acceptance criteria

- all milestones complete before final task review.

` + section + `## Historical invariants

- task-wide authority remains the active task.
`
}
