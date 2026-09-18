package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ProjectContinuation = projectContinuationObligation

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool                              `json:"completion_admitted"`
	StopAdmitted       bool                              `json:"stop_admitted"`
	Continuation       ProjectContinuation               `json:"continuation"`
	TaskAttribution    repositoryproject.TaskAttribution `json:"task_attribution"`
}

const (
	projectContinuationReasonPostCompletionActive = repositoryproject.ReasonPostCompletionActive
	projectContinuationActionStart                = repositoryproject.ActionStart
)

func BuildCurrentParentRequestCompletionProjection(cfg config.AppConfig, st *state.StateStore) (ParentRequestCompletionProjection, error) {
	status := st.TaskStatus()
	var projection ParentRequestCompletionProjection
	var err error
	if status == state.TaskStatusAwaitingParentCompletion || status == state.TaskStatusComplete {
		projection, err = BuildParentRequestCompletionProjection(cfg, st.ReadOr("active-task", ""))
	} else {
		var output projectStateOutput
		output, err = buildProjectState(cfg, st)
		if err == nil {
			projection = parentRequestProjection(output.Continuation)
			projection.TaskAttribution, err = repositoryprojecttree.BuildTaskAttribution(
				cfg.RepoRoot,
				st.ReadOr("active-task", ""),
				projectContinuationToPolicy(output.Continuation),
			)
		}
	}
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if authorityTask, authorityErr := st.CurrentTaskAuthorityPath(); authorityErr == nil {
		projection.TaskAttribution = repositoryproject.BindTaskAuthority(projection.TaskAttribution, authorityTask)
	} else if projection.TaskAttribution.Handover {
		projection.TaskAttribution = repositoryproject.BindTaskAuthority(projection.TaskAttribution, "")
	}
	return projection, nil
}

func BuildParentRequestCompletionProjection(cfg config.AppConfig, completedTask string) (ParentRequestCompletionProjection, error) {
	policyProjection, err := repositoryprojecttree.BuildParentRequestCompletionProjection(cfg.RepoRoot, completedTask)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	projection := parentRequestProjectionFromPolicy(policyProjection)
	attribution, err := repositoryprojecttree.BuildTaskAttribution(cfg.RepoRoot, completedTask, policyProjection.Continuation)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	projection.TaskAttribution = attribution
	return projection, nil
}

func parentRequestProjectionFromPolicy(projection repositoryproject.ParentRequestCompletionProjection) ParentRequestCompletionProjection {
	return ParentRequestCompletionProjection{
		CompletionAdmitted: projection.CompletionAdmitted,
		StopAdmitted:       projection.StopAdmitted,
		Continuation:       projectContinuationFromPolicy(projection.Continuation),
	}
}

func parentRequestProjection(continuation ProjectContinuation) ParentRequestCompletionProjection {
	policy := repositoryproject.ParentRequestProjection(
		projectContinuationToPolicy(continuation),
		continuation.Reason != string(state.TaskStatusRateLimited),
	)
	projection := parentRequestProjectionFromPolicy(policy)
	projection.Continuation.Automation = continuation.Automation
	return projection
}
