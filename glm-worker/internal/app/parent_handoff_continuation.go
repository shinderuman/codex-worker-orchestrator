package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func applyParentRequestCompletion(repoRoot string, st *state.StateStore, output *parentHandoffOutput) {
	if repoRoot == "" {
		return
	}
	active, err := workflow.RepositoryHarnessActive(repoRoot, st)
	if err != nil {
		markHandoffInconsistent(output, "repository harness activation is unavailable: "+err.Error())
		return
	}
	if !active {
		return
	}
	projection, err := BuildCurrentParentRequestCompletionProjection(config.AppConfig{RepoRoot: repoRoot}, st)
	if err != nil {
		markHandoffInconsistent(output, "project continuation projection is unavailable: "+err.Error())
		return
	}
	output.ParentRequest = &projection
	if projection.TaskAttribution.Reason == repositoryproject.ReasonActiveTaskMismatch && !projection.TaskAttribution.Handover {
		markHandoffInconsistent(output, "canonical handoff task attribution mismatch: lifecycle="+projection.TaskAttribution.LifecycleTask+", authority="+projection.TaskAttribution.AuthorityTask+", active="+projection.TaskAttribution.ActiveTask)
		return
	}
	validateParentContinuationActionability(st, output)
}

func validateParentContinuationActionability(st *state.StateStore, output *parentHandoffOutput) {
	if !fatalActiveContinuationWithoutAction(output) {
		return
	}
	if latestParentMaterialOutcome(st) != "error" {
		return
	}
	markHandoffInconsistent(output, "active task has a terminal error while project continuation is required but no parent action is admitted")
}

func fatalActiveContinuationWithoutAction(output *parentHandoffOutput) bool {
	return output.Consistent &&
		output.TaskStatus != nil && *output.TaskStatus == string(state.TaskStatusActive) &&
		output.RequiredAction != nil && *output.RequiredAction == string(state.ParentActionNone) &&
		len(output.AllowedActions) == 0 &&
		output.ParentRequest != nil && output.ParentRequest.Continuation.State == projectContinuationContinueNow
}

func latestParentMaterialOutcome(st *state.StateStore) string {
	taskID := st.ReadOr("task.id", "")
	logs, err := taskview.ReadStatusTelemetry(st, taskID)
	if err != nil {
		return ""
	}
	for index := len(logs) - 1; index >= 0; index-- {
		if logs[index].CallType != state.CallTypeProbe {
			return logs[index].Outcome
		}
	}
	return ""
}
