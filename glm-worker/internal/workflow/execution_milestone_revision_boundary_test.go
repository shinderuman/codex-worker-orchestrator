package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionMilestoneRevisionAllowsNaturalParentBoundaries(t *testing.T) {
	for _, status := range []state.TaskStatus{
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusInterrupted,
	} {
		if !executionMilestoneRevisionStatusAllowed(status) {
			t.Fatalf("natural parent boundary %q rejected", status)
		}
	}
	for _, status := range []state.TaskStatus{state.TaskStatusActive, state.TaskStatusComplete, state.TaskStatusNone} {
		if executionMilestoneRevisionStatusAllowed(status) {
			t.Fatalf("non-boundary status %q accepted", status)
		}
	}
}
