package app

import (
	"strings"
	"testing"
)

func TestParentRequestCompletionProjectionRequiresImmediateNextTask(t *testing.T) {
	cfg := newAppConfig(t)
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{next}, nil, nil))
	writeProjectContinuationTask(t, cfg, next)

	projection, err := BuildParentRequestCompletionProjection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if projection.CompletionAdmitted || projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationContinueNow ||
		projection.Continuation.Task != next ||
		projection.Continuation.RequiredAction != projectContinuationActionStart ||
		projection.Continuation.Reason != projectContinuationReasonPostCompletionActive {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionAllowsBlockedStopWithoutCompletion(t *testing.T) {
	cfg := newAppConfig(t)
	blocked := "IMPLEMENTATION_TASKS/blocked.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", nil, nil, []string{blocked}))
	writeProjectContinuationTask(t, cfg, blocked)

	projection, err := BuildParentRequestCompletionProjection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationBlocked ||
		projection.Continuation.Task != blocked || projection.Continuation.Blocker == nil ||
		projection.Continuation.Blocker.Task != blocked {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionAdmitsCompletedGoalOnly(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("completed", nil, nil, nil))

	projection, err := BuildParentRequestCompletionProjection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationTerminal ||
		projection.Continuation.Reason != projectContinuationReasonGoalCompleted {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionDoesNotInventLegacyScope(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/active.md"
	plan := "# Plan\n\n## ACTIVE\n\n- `" + active + "`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", plan)
	writeProjectContinuationTask(t, cfg, active)

	projection, err := BuildParentRequestCompletionProjection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if projection.CompletionAdmitted || projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationUnknown ||
		projection.Continuation.Reason != projectContinuationReasonContinuationScopeUnbound {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionRejectsCompletedGoalWithRemainingSchedule(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/active.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("completed", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)

	_, err := BuildParentRequestCompletionProjection(cfg)
	if err == nil || !strings.Contains(err.Error(), "空にする必要があります") {
		t.Fatalf("err = %v", err)
	}
}
