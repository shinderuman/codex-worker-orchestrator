package parentactioncmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

func TestContinuationStopBlockReason(t *testing.T) {
	tests := []struct {
		name      string
		handoff   continuationGateHandoff
		loadErr   error
		wantBlock bool
		wantText  string
	}{
		{
			name:    "inactive repository harness",
			handoff: continuationGateHandoff{Consistent: true},
		},
		{
			name: "terminal goal",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationTerminal, repositoryproject.ReasonGoalCompleted, true, true,
			)},
		},
		{
			name: "explicit user stop",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationExplicitStop, repositoryproject.ReasonUserInterruption, false, true,
			)},
		},
		{
			name: "verified automation deferral",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationDeferredByVerifiedAutomation, "verified-automation", false, true,
			)},
		},
		{
			name: "blocked boundary",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationBlocked, "user-decision", false, true,
			)},
		},
		{
			name: "non-goal post-completion stop admitted",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationContinueNow, repositoryproject.ReasonPostCompletionActive, true, true,
			)},
		},
		{
			name: "active task mismatch",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: &app.ParentRequestCompletionProjection{
				Continuation: app.ProjectContinuation{
					State:          repositoryproject.ContinuationUnknown,
					Reason:         repositoryproject.ReasonActiveTaskMismatch,
					Task:           "IMPLEMENTATION_TASKS/current.md",
					RequiredAction: repositoryproject.ActionStart,
				},
			}},
			wantBlock: true,
			wantText:  "reason=active-task-mismatch",
		},
		{
			name: "continue now",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: &app.ParentRequestCompletionProjection{
				Continuation: app.ProjectContinuation{
					State:          repositoryproject.ContinuationContinueNow,
					Reason:         repositoryproject.ReasonNextRunnable,
					Task:           "IMPLEMENTATION_TASKS/next.md",
					RequiredAction: repositoryproject.ActionStart,
				},
			}},
			wantBlock: true,
			wantText:  "required_action=start",
		},
		{
			name:      "handoff unavailable",
			loadErr:   errors.New("boom"),
			wantBlock: true,
			wantText:  "gate unavailable",
		},
		{
			name: "inconsistent handoff",
			handoff: continuationGateHandoff{
				Consistent:    false,
				Inconsistency: stringPointer("session rotation projection unavailable"),
			},
			wantBlock: true,
			wantText:  "session rotation projection unavailable",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason := continuationStopBlockReason(tc.handoff, tc.loadErr)
			if got := reason != ""; got != tc.wantBlock {
				t.Fatalf("blocked=%t reason=%q wantBlock=%t", got, reason, tc.wantBlock)
			}
			if tc.wantText != "" && !strings.Contains(reason, tc.wantText) {
				t.Fatalf("reason=%q want substring %q", reason, tc.wantText)
			}
		})
	}
}

func TestContinuationMetadataGuardFailure(t *testing.T) {
	tests := []struct {
		name    string
		handoff continuationGateHandoff
		loadErr error
		wantErr string
	}{
		{
			name:    "inactive repository harness",
			handoff: continuationGateHandoff{Consistent: true},
		},
		{
			name: "continue now permits metadata advancement",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationContinueNow, repositoryproject.ReasonNextRunnable, false, false,
			)},
		},
		{
			name: "blocked admitted boundary",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationBlocked, "user-decision", false, true,
			)},
		},
		{
			name: "terminal goal",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationTerminal, repositoryproject.ReasonGoalCompleted, true, true,
			)},
		},
		{
			name: "explicit stop",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationExplicitStop, repositoryproject.ReasonUserInterruption, false, true,
			)},
		},
		{
			name: "verified automation deferral",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationDeferredByVerifiedAutomation, "verified-automation", false, true,
			)},
		},
		{
			name: "active task mismatch rejected",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationUnknown, repositoryproject.ReasonActiveTaskMismatch, false, false,
			)},
			wantErr: "active-task-mismatch",
		},
		{
			name: "unverified blocked stop rejected",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationBlocked, "rate-limited", false, false,
			)},
			wantErr: "stop_admitted=false",
		},
		{
			name: "broken terminal projection rejected",
			handoff: continuationGateHandoff{Consistent: true, ParentRequest: projection(
				repositoryproject.ContinuationTerminal, repositoryproject.ReasonGoalCompleted, false, true,
			)},
			wantErr: "completion_admitted=false",
		},
		{
			name:    "handoff unavailable",
			loadErr: errors.New("boom"),
			wantErr: "guard unavailable",
		},
		{
			name: "inconsistent handoff",
			handoff: continuationGateHandoff{
				Consistent:    false,
				Inconsistency: stringPointer("active task state invalid"),
			},
			wantErr: "active task state invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := continuationMetadataGuardFailure(tc.handoff, tc.loadErr)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v want substring %q", err, tc.wantErr)
			}
		})
	}
}

func projection(state, reason string, completionAdmitted, stopAdmitted bool) *app.ParentRequestCompletionProjection {
	return &app.ParentRequestCompletionProjection{
		CompletionAdmitted: completionAdmitted,
		StopAdmitted:       stopAdmitted,
		Continuation: app.ProjectContinuation{
			State:  state,
			Reason: reason,
		},
	}
}

func stringPointer(value string) *string {
	return &value
}
