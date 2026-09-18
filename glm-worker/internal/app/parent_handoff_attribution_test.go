package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffCarriesLegalPostCompletionTaskHandover(t *testing.T) {
	cfg := newAppConfig(t)
	promoted := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan([]string{promoted}, nil, nil))
	writeProjectContinuationTask(t, cfg, promoted)
	st := startActivatedParentHandoffTask(t, cfg)
	if err := st.Write("active-task", completedNonGoalTask); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if !output.Consistent || output.ParentRequest == nil {
		t.Fatalf("handoff = %#v", output)
	}
	attribution := output.ParentRequest.TaskAttribution
	if attribution.LifecycleTask != completedNonGoalTask || attribution.ActiveTask != promoted ||
		attribution.Matches || !attribution.Handover || attribution.Reason != repositoryproject.ReasonPostCompletionActive ||
		attribution.LegalNextAction != repositoryproject.ActionStart {
		t.Fatalf("task attribution = %#v", attribution)
	}
}

func TestParentHandoffFailsClosedOnUnrelatedStaleTaskAttribution(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st := startActivatedParentHandoffTask(t, cfg)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/stale.md"); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Consistent || output.ParentRequest == nil || output.Inconsistency == nil {
		t.Fatalf("handoff = %#v", output)
	}
	attribution := output.ParentRequest.TaskAttribution
	if attribution.LifecycleTask != "IMPLEMENTATION_TASKS/stale.md" || attribution.ActiveTask != active ||
		attribution.Matches || attribution.Handover || attribution.Reason != repositoryproject.ReasonActiveTaskMismatch ||
		attribution.LegalNextAction != "" {
		t.Fatalf("task attribution = %#v", attribution)
	}
	if !strings.Contains(*output.Inconsistency, "task attribution mismatch") {
		t.Fatalf("inconsistency = %q", *output.Inconsistency)
	}
}
