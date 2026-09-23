package executionmilestone

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRevisionStatusAllowsNaturalParentBoundaries(t *testing.T) {
	for _, status := range []state.TaskStatus{
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusQualityGateRecoverable,
		state.TaskStatusInterrupted,
	} {
		if !revisionStatusAllowed(status) {
			t.Fatalf("natural parent boundary %q rejected", status)
		}
	}
	for _, status := range []state.TaskStatus{state.TaskStatusActive, state.TaskStatusComplete, state.TaskStatusNone} {
		if revisionStatusAllowed(status) {
			t.Fatalf("non-boundary status %q accepted", status)
		}
	}
}
