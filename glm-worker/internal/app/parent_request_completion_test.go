package app

import (
	"strings"
	"testing"
)

const completedNonGoalTask = "IMPLEMENTATION_TASKS/active.md"

func TestParentRequestCompletionProjectionRequiresImmediateNextTask(t *testing.T) {
	cfg := newAppConfig(t)
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{next}, nil, nil))
	writeProjectContinuationTask(t, cfg, next)

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
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

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
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

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationTerminal ||
		projection.Continuation.Reason != projectContinuationReasonGoalCompleted {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionAdmitsNonGoalPromotedActiveWithoutStart(t *testing.T) {
	cfg := newAppConfig(t)
	promoted := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan([]string{promoted}, nil, nil))
	writeProjectContinuationTask(t, cfg, promoted)

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationContinueNow ||
		projection.Continuation.Task != promoted ||
		projection.Continuation.RequiredAction != projectContinuationActionStart ||
		projection.Continuation.Reason != projectContinuationReasonPostCompletionActive {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionAdmitsNonGoalBlockedOnlyCompletion(t *testing.T) {
	cfg := newAppConfig(t)
	blocked := "IMPLEMENTATION_TASKS/blocked.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan(nil, nil, []string{blocked}))
	writeProjectContinuationTask(t, cfg, blocked)

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationBlocked ||
		projection.Continuation.Task != blocked || projection.Continuation.Blocker == nil ||
		projection.Continuation.Blocker.Task != blocked ||
		projection.Continuation.RequiredAction != "" {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionAdmitsNonGoalExhaustedSchedule(t *testing.T) {
	cfg := newAppConfig(t)
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan(nil, nil, nil))

	projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.CompletionAdmitted || !projection.StopAdmitted ||
		projection.Continuation.State != projectContinuationTerminal ||
		projection.Continuation.Reason != projectContinuationReasonScheduleExhausted {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestParentRequestCompletionProjectionDeniesNonGoalPreSyncSchedule(t *testing.T) {
	cases := []struct {
		name    string
		active  []string
		next    []string
		blocked []string
		files   []string
	}{
		{
			name:   "completed task still active with next",
			active: []string{completedNonGoalTask},
			next:   []string{"IMPLEMENTATION_TASKS/next.md"},
			files:  []string{completedNonGoalTask, "IMPLEMENTATION_TASKS/next.md"},
		},
		{
			name:    "completed task still active with blocked",
			active:  []string{completedNonGoalTask},
			blocked: []string{"IMPLEMENTATION_TASKS/blocked.md"},
			files:   []string{completedNonGoalTask, "IMPLEMENTATION_TASKS/blocked.md"},
		},
		{
			name:   "completed task still active alone",
			active: []string{completedNonGoalTask},
			files:  []string{completedNonGoalTask},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan(tc.active, tc.next, tc.blocked))
			for _, path := range tc.files {
				writeProjectContinuationTask(t, cfg, path)
			}

			projection, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
			if err != nil {
				t.Fatal(err)
			}
			if projection.CompletionAdmitted || projection.StopAdmitted ||
				projection.Continuation.State != projectContinuationUnknown ||
				projection.Continuation.Reason != projectContinuationReasonContinuationScopeUnbound ||
				projection.Continuation.Task != "" ||
				projection.Continuation.RequiredAction != "" {
				t.Fatalf("projection = %#v", projection)
			}
		})
	}
}

func TestParentRequestCompletionProjectionRejectsNonGoalPromotionGap(t *testing.T) {
	cfg := newAppConfig(t)
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan(nil, []string{next}, nil))
	writeProjectContinuationTask(t, cfg, next)

	_, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err == nil || !strings.Contains(err.Error(), "ACTIVE昇格済み") {
		t.Fatalf("err = %v", err)
	}
}

func TestParentRequestCompletionProjectionRejectsCompletedGoalWithRemainingSchedule(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/active.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("completed", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)

	_, err := BuildParentRequestCompletionProjection(cfg, completedNonGoalTask)
	if err == nil || !strings.Contains(err.Error(), "空にする必要があります") {
		t.Fatalf("err = %v", err)
	}
}
