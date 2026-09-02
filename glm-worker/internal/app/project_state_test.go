package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeProjectStateRepoFile(t *testing.T, repoRoot, path, content string) {
	t.Helper()
	target := filepath.Join(repoRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitProjectStateRepo(t *testing.T, repoRoot string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "fixture"},
	} {
		command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func projectStateTaskBody(dependencies string) string {
	return "# Task\n\n## Review findings\n\nnone\n\n## Dependencies\n\n" + dependencies + "\n"
}

func TestProjectStatePlanAbsent(t *testing.T) {
	cfg := newAppConfig(t)
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if output.PlanPresent || output.Goal != nil || output.Schedule != nil || output.NextRunnable != nil || output.Completion != nil {
		t.Fatalf("output = %#v", output)
	}
}

func TestProjectStateLegacyPlanWithoutGoal(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/first.md`\n\n## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/second.md`\n- `IMPLEMENTATION_TASKS/third.md`\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/fourth.md`\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/first.md", projectStateTaskBody("none"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/second.md", projectStateTaskBody("none"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/third.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/second.md`"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/fourth.md", projectStateTaskBody("none"))
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if !output.PlanPresent || output.Goal == nil || output.Goal.Present {
		t.Fatalf("goal = %#v", output.Goal)
	}
	if output.Schedule == nil || len(output.Schedule.Active) != 1 || output.Schedule.Active[0] != "IMPLEMENTATION_TASKS/first.md" {
		t.Fatalf("schedule = %#v", output.Schedule)
	}
	if output.NextRunnable == nil || *output.NextRunnable != "IMPLEMENTATION_TASKS/second.md" {
		t.Fatalf("next_runnable = %v", output.NextRunnable)
	}
	third := projectStateDependencyOf(t, output.Dependencies, "IMPLEMENTATION_TASKS/third.md")
	if len(third.Outstanding) != 1 || third.Outstanding[0] != "IMPLEMENTATION_TASKS/second.md" {
		t.Fatalf("third = %#v", third)
	}
	if len(output.Blockers) != 2 || output.Blockers[0].Task != "IMPLEMENTATION_TASKS/third.md" || output.Blockers[1].Task != "IMPLEMENTATION_TASKS/fourth.md" {
		t.Fatalf("blockers = %#v", output.Blockers)
	}
	if output.Completion != nil {
		t.Fatalf("completion = %#v", output.Completion)
	}
}

func projectStateDependencyOf(t *testing.T, dependencies []projectStateDependency, task string) projectStateDependency {
	t.Helper()
	for _, dependency := range dependencies {
		if dependency.Task == task {
			return dependency
		}
	}
	t.Fatalf("dependency %s not found in %#v", task, dependencies)
	return projectStateDependency{}
}

func TestProjectStateGoalActiveCompletionUnmet(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/final.md", projectStateTaskBody("none"))
	commitProjectStateRepo(t, cfg.RepoRoot)
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if output.Completion == nil || output.Completion.Ready {
		t.Fatalf("completion = %#v", output.Completion)
	}
	unmet := strings.Join(output.Completion.Unmet, ",")
	for _, want := range []string{"task_not_complete", "active_task_mismatch", "validation_not_current"} {
		if !strings.Contains(unmet, want) {
			t.Fatalf("unmet = %q", unmet)
		}
	}
	if output.Completion.TreeClean == nil || !*output.Completion.TreeClean {
		t.Fatalf("tree_clean = %v", output.Completion.TreeClean)
	}
}

func TestProjectStateGoalCompletionReady(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/final.md", projectStateTaskBody("none"))
	commitProjectStateRepo(t, cfg.RepoRoot)
	output, err := buildProjectState(cfg, prepareCompletedGoalTaskState(t, cfg, "IMPLEMENTATION_TASKS/final.md"))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if output.Completion == nil || !output.Completion.Ready || len(output.Completion.Unmet) != 0 {
		t.Fatalf("completion = %#v", output.Completion)
	}
	if output.Completion.TaskStatus != string(state.TaskStatusComplete) || output.Completion.RequiredAction != string(state.ParentActionNone) {
		t.Fatalf("completion = %#v", output.Completion)
	}
	if len(output.Completion.Validations) != 1 || output.Completion.Validations[0].Form != "go-test" {
		t.Fatalf("validations = %#v", output.Completion.Validations)
	}
}

func prepareCompletedGoalTaskState(t *testing.T, cfg config.AppConfig, taskPath string) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", taskPath); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQualityGateRun(st, qualityGateRunRecord{
		ValidationRunID: strings.Repeat("a", 32),
		Form:            "go-test",
		Repository:      cfg.RepoRoot,
		WorkingDir:      cfg.RepoRoot,
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          qualityGateStatusPass,
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestProjectStateGoalProgressionToTerminal(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/first.md`\n\n## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/second.md`\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/first.md", projectStateTaskBody("none"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/second.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/first.md`"))
	commitProjectStateRepo(t, cfg.RepoRoot)
	mid, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if mid.NextRunnable != nil {
		t.Fatalf("next_runnable = %v", mid.NextRunnable)
	}
	if len(mid.Blockers) != 1 || mid.Blockers[0].Task != "IMPLEMENTATION_TASKS/second.md" || len(mid.Blockers[0].Outstanding) != 1 {
		t.Fatalf("blockers = %#v", mid.Blockers)
	}
	if mid.Completion == nil || mid.Completion.Ready || !strings.Contains(strings.Join(mid.Completion.Unmet, ","), "schedule_not_empty") {
		t.Fatalf("completion = %#v", mid.Completion)
	}

	if err := os.Remove(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS", "first.md")); err != nil {
		t.Fatal(err)
	}
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/second.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	commitProjectStateRepo(t, cfg.RepoRoot)
	ready, err := buildProjectState(cfg, prepareCompletedGoalTaskState(t, cfg, "IMPLEMENTATION_TASKS/second.md"))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if ready.Completion == nil || !ready.Completion.Ready || len(ready.Completion.Unmet) != 0 {
		t.Fatalf("completion = %#v", ready.Completion)
	}
	second := projectStateDependencyOf(t, ready.Dependencies, "IMPLEMENTATION_TASKS/second.md")
	if len(second.Fulfilled) != 1 || second.Fulfilled[0] != "IMPLEMENTATION_TASKS/first.md" || len(second.Outstanding) != 0 {
		t.Fatalf("second = %#v", second)
	}

	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: completed\n\ncompletion decision: acceptance met\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	terminal, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if terminal.Goal == nil || terminal.Goal.Status != "completed" {
		t.Fatalf("goal = %#v", terminal.Goal)
	}
	if len(terminal.Schedule.Active) != 0 || len(terminal.Schedule.Next) != 0 || terminal.NextRunnable != nil || terminal.Completion != nil {
		t.Fatalf("terminal = %#v", terminal)
	}
}

func TestProjectStateGoalCompletedEmptySchedule(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: completed\n\ncompletion decision: acceptance met\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	if output.Goal == nil || output.Goal.Status != "completed" {
		t.Fatalf("goal = %#v", output.Goal)
	}
	if output.Schedule == nil || len(output.Schedule.Active) != 0 || len(output.Schedule.Next) != 0 || len(output.Schedule.Blocked) != 0 {
		t.Fatalf("schedule = %#v", output.Schedule)
	}
	if output.NextRunnable != nil || output.Completion != nil {
		t.Fatalf("output = %#v", output)
	}
}

func TestProjectStateGoalCompletedWithScheduleFailsClosed(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: completed\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/final.md", projectStateTaskBody("none"))
	if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
		t.Fatal("completed GOAL with scheduled entries was accepted")
	}
}

func TestProjectStateGoalActiveWithoutActiveFailsClosed(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
		t.Fatal("active GOAL without ACTIVE task was accepted")
	}
}

func TestProjectStateMalformedScheduleFailsClosed(t *testing.T) {
	for name, plan := range map[string]string{
		"active goal with non-bullet NEXT line":       "# Plan\n\n## GOAL\n\nstatus: active\n\nGoal原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\nplain prose line\n\n## BLOCKED / USER_PERMISSION_WAIT\n",
		"completed goal with non-bullet BLOCKED line": "# Plan\n\n## GOAL\n\nstatus: completed\n\ncompletion decision: acceptance met\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\nplain prose line\n",
		"legacy plan with malformed NEXT bullet":      "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/final.md`\n\n## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/final.md` は次\n\n## BLOCKED / USER_PERMISSION_WAIT\n",
	} {
		cfg := newAppConfig(t)
		writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", plan)
		if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestProjectStateFulfilledDependencyViaGitHistory(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/first.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/first.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/done.md`"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/done.md", projectStateTaskBody("none"))
	commitProjectStateRepo(t, cfg.RepoRoot)
	if err := os.Remove(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS", "done.md")); err != nil {
		t.Fatal(err)
	}
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatalf("buildProjectState: %v", err)
	}
	first := projectStateDependencyOf(t, output.Dependencies, "IMPLEMENTATION_TASKS/first.md")
	if len(first.Fulfilled) != 1 || first.Fulfilled[0] != "IMPLEMENTATION_TASKS/done.md" || len(first.Outstanding) != 0 {
		t.Fatalf("first = %#v", first)
	}
}

func TestProjectStateUnknownDependencyFailsClosed(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/first.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/first.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/ghost.md`"))
	commitProjectStateRepo(t, cfg.RepoRoot)
	if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
		t.Fatal("unknown dependency was accepted")
	}
}

func TestProjectStateSelfDependencyFailsClosed(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/first.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/first.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/first.md`"))
	if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
		t.Fatal("self dependency was accepted")
	}
}

func TestProjectStateDependencyCycleFailsClosed(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/b.md`\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/a.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/b.md`"))
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/b.md", projectStateTaskBody("- `IMPLEMENTATION_TASKS/a.md`"))
	if _, err := buildProjectState(cfg, state.AttachStateStore(cfg)); err == nil {
		t.Fatal("dependency cycle was accepted")
	}
}

func TestProjectStateParseCommand(t *testing.T) {
	cmd, err := ParseCommand([]string{"--project-state"})
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if cmd.Mode != ModeProjectState {
		t.Fatalf("mode = %d", cmd.Mode)
	}
	if _, err := ParseCommand([]string{"--project-state", "extra"}); err == nil {
		t.Fatal("extra argument was accepted")
	}
}
