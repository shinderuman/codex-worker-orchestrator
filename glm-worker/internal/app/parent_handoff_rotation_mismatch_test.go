package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffRotationPendingMismatchKeepsRecoveryDirective(t *testing.T) {
	cfg, st, _ := seedSessionRotationAccept(t)
	promoted := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan([]string{promoted}, nil, nil))
	writeProjectContinuationTask(t, cfg, promoted)
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	saveHandoffTaskAuthority(t, st, completedNonGoalTask)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/stale.md"); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Consistent || output.Inconsistency == nil || output.ParentRequest == nil || output.SessionRotation == nil ||
		output.SessionRotation.State != state.SessionRotationProjectionPending || output.SessionRotation.Directive == nil {
		t.Fatalf("handoff = %#v", output)
	}
	attribution := output.ParentRequest.TaskAttribution
	if attribution.LifecycleTask != "IMPLEMENTATION_TASKS/stale.md" || attribution.AuthorityTask != completedNonGoalTask ||
		attribution.ActiveTask != promoted || attribution.Matches || attribution.Handover ||
		attribution.Reason != repositoryproject.ReasonActiveTaskMismatch || attribution.LegalNextAction != "" {
		t.Fatalf("task attribution = %#v", attribution)
	}

	recovery := projectParentHandoffRecovery(output)
	if recovery.Consistent || recovery.SessionRotation == nil || recovery.SessionRotation.State != state.SessionRotationProjectionPending ||
		recovery.SessionRotation.Directive == nil || recovery.SessionRotation.Directive.DirectiveID != output.SessionRotation.Directive.DirectiveID ||
		recovery.ParentRequest == nil || recovery.ParentRequest.TaskAttribution.Reason != repositoryproject.ReasonActiveTaskMismatch {
		t.Fatalf("recovery = %#v", recovery)
	}
}
