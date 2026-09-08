package parentactioncmd

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const (
	pushBindingFailureParentCompletionNotReady    = "parent_completion_not_ready"
	pushBindingFailureParentCompletionStateError  = "parent_completion_state_unreadable"
	pushBindingFailureParentCompletionHeadChanged = "parent_completion_head_changed"
)

func applyCurrentParentPushAuthorization(repoRoot string, output *pushBindingOutput) {
	if output == nil || output.RemoteWrite == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		blockPushBindingRemoteWrite(output, &finalizationFailure{
			Stage: "authorization", Reason: pushBindingFailureParentCompletionStateError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		})
		return
	}
	if cfg.RepoRoot != repoRoot {
		return
	}
	if failure := parentPushAuthorizationFailure(cfg, output.ExpectedOID); failure != nil {
		blockPushBindingRemoteWrite(output, failure)
	}
}

func blockPushBindingRemoteWrite(output *pushBindingOutput, failure *finalizationFailure) {
	output.Status = completePushStatusBlocked
	output.RemoteWrite = nil
	output.Failure = failure
}

func parentPushAuthorizationFailure(cfg config.AppConfig, expectedOID string) *finalizationFailure {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return &finalizationFailure{
			Stage: "authorization", Reason: pushBindingFailureParentCompletionStateError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return &finalizationFailure{
			Stage: "authorization", Reason: pushBindingFailureParentCompletionStateError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	defer func() { _ = lock.Close() }()

	status := st.TaskStatus()
	if status == state.TaskStatusNone || status == state.TaskStatusComplete {
		return nil
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return &finalizationFailure{
			Stage: "authorization", Reason: pushBindingFailureParentCompletionStateError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	if !plan.Allows(state.ParentActionComplete) {
		return &finalizationFailure{Stage: "authorization", Reason: pushBindingFailureParentCompletionNotReady}
	}
	if !pushBindingTreeClean(cfg.RepoRoot) {
		return &finalizationFailure{Stage: "git", Reason: "tree_not_clean"}
	}
	if _, err := workflow.CheckParentCompletionHead(cfg.RepoRoot); err != nil {
		return &finalizationFailure{
			Stage: "metadata", Reason: "completion_transition_invalid", Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	headOID, unborn, failure := completeHeadState(cfg.RepoRoot)
	if failure != nil {
		return failure
	}
	if unborn || !strings.EqualFold(headOID, expectedOID) {
		return &finalizationFailure{
			Stage: "authorization", Reason: pushBindingFailureParentCompletionHeadChanged,
			Detail: compactFinalizationDiagnostic(fmt.Sprintf("expected %s observed %s", expectedOID, headOID)),
		}
	}
	return verifyCompletedTaskFileRemoved(cfg.RepoRoot, st, headOID)
}
