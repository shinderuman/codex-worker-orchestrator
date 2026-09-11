package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func projectContinuationPlan(goalStatus string, active, next, blocked []string) string {
	var body strings.Builder
	body.WriteString("# Plan\n\n## GOAL\n\nstatus: ")
	body.WriteString(goalStatus)
	body.WriteString("\n\nGoal fixture\n\n## ACTIVE\n\n")
	writeProjectContinuationEntries(&body, active)
	body.WriteString("\n## NEXT（優先順）\n\n")
	writeProjectContinuationEntries(&body, next)
	body.WriteString("\n## BLOCKED / USER_PERMISSION_WAIT\n\n")
	writeProjectContinuationEntries(&body, blocked)
	return body.String()
}

func writeProjectContinuationEntries(body *strings.Builder, entries []string) {
	for _, entry := range entries {
		body.WriteString("- `")
		body.WriteString(entry)
		body.WriteString("`\n")
	}
}

func writeProjectContinuationTask(t *testing.T, cfg config.AppConfig, path string) {
	t.Helper()
	writeProjectStateRepoFile(t, cfg.RepoRoot, path, projectStateTaskBody("none"))
}

func TestProjectContinuationNoRuntimeTaskUsesCurrentActiveTask(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	output, err := buildProjectState(cfg, state.AttachStateStore(cfg))
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, state.AttachStateStore(cfg))
	if obligation.State != projectContinuationContinueNow || obligation.Task != active || obligation.Reason != projectContinuationReasonActiveTaskNotStarted {
		t.Fatalf("continuation = %#v", obligation)
	}
}

func TestProjectContinuationCurrentRuntimeTaskStaysCurrent(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, []string{next}, nil))
	writeProjectContinuationTask(t, cfg, active)
	writeProjectContinuationTask(t, cfg, next)
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
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationContinueNow || obligation.Task != active || obligation.Reason != projectContinuationReasonCurrentTask {
		t.Fatalf("continuation = %#v", obligation)
	}
}

func TestProjectContinuationUsesCanonicalNextRunnableAfterCurrentCompletion(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, []string{next}, nil))
	writeProjectContinuationTask(t, cfg, active)
	writeProjectContinuationTask(t, cfg, next)
	commitProjectStateRepo(t, cfg.RepoRoot)
	st := prepareCompletedGoalTaskState(t, cfg, active)
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, st)
	if output.NextRunnable == nil || obligation.State != projectContinuationContinueNow || obligation.Task != *output.NextRunnable || obligation.Reason != projectContinuationReasonNextRunnable {
		t.Fatalf("continuation = %#v next=%v", obligation, output.NextRunnable)
	}
}

func TestProjectContinuationBlockedOnlyUsesCanonicalBlocker(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	blocked := "IMPLEMENTATION_TASKS/blocked.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, []string{blocked}))
	writeProjectContinuationTask(t, cfg, active)
	writeProjectContinuationTask(t, cfg, blocked)
	commitProjectStateRepo(t, cfg.RepoRoot)
	st := prepareCompletedGoalTaskState(t, cfg, active)
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationBlocked || obligation.Task != blocked || obligation.Reason != "blocked-section" || obligation.Blocker == nil || obligation.Blocker.Task != blocked {
		t.Fatalf("continuation = %#v blockers=%#v", obligation, output.Blockers)
	}
}

func TestProjectContinuationTerminalRequiresCompletedGoalAndSettledLifecycle(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("completed", nil, nil, nil))
	st := state.AttachStateStore(cfg)
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationTerminal || obligation.Reason != projectContinuationReasonGoalCompleted {
		t.Fatalf("continuation = %#v", obligation)
	}

	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	obligation = deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationUnknown || obligation.Reason != projectContinuationReasonGoalLifecycleInconsistent {
		t.Fatalf("active runtime with completed goal = %#v", obligation)
	}
}

func TestProjectContinuationInterruptedTaskIsExplicitStop(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
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
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:    state.ResumeStageWorker,
		Phase:    "worker-new",
		Role:     state.WorkerRole,
		Model:    "opus",
		Request:  "request",
		StopKind: state.ResumeStopInterrupted,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	obligation := deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationExplicitStop || obligation.Task != active || obligation.RequiredAction != string(state.ParentActionResume) || obligation.Reason != projectContinuationReasonUserInterruption {
		t.Fatalf("continuation = %#v", obligation)
	}
}

func TestProjectContinuationLegacyNextDoesNotCreateContinuationScope(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	next := "IMPLEMENTATION_TASKS/unscoped-next.md"
	plan := "# Plan\n\n## ACTIVE\n\n- `" + active + "`\n\n## NEXT（優先順）\n\n- `" + next + "`\n\n## BLOCKED / USER_PERMISSION_WAIT\n"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", plan)
	writeProjectContinuationTask(t, cfg, active)
	writeProjectContinuationTask(t, cfg, next)
	commitProjectStateRepo(t, cfg.RepoRoot)
	st := prepareCompletedGoalTaskState(t, cfg, active)
	output, err := buildProjectState(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if output.NextRunnable == nil || *output.NextRunnable != next {
		t.Fatalf("next_runnable = %v", output.NextRunnable)
	}
	obligation := deriveProjectContinuation(output, st)
	if obligation.State != projectContinuationUnknown || obligation.Task != "" || obligation.Reason != projectContinuationReasonContinuationScopeUnbound {
		t.Fatalf("continuation = %#v", obligation)
	}
}

func TestProjectStateJSONIncludesCanonicalContinuation(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	var stdout bytes.Buffer
	if err := printProjectState(cfg, state.AttachStateStore(cfg), &stdout); err != nil {
		t.Fatal(err)
	}
	var output struct {
		Continuation projectContinuationObligation `json:"continuation"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Continuation.State != projectContinuationContinueNow || output.Continuation.Task != active {
		t.Fatalf("continuation JSON = %#v body=%s", output.Continuation, stdout.String())
	}
}
