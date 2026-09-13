package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ProjectContinuation = projectContinuationObligation

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool                `json:"completion_admitted"`
	StopAdmitted       bool                `json:"stop_admitted"`
	Continuation       ProjectContinuation `json:"continuation"`
}

const (
	projectContinuationReasonPostCompletionActive = repositoryproject.ReasonPostCompletionActive
	projectContinuationActionStart                = repositoryproject.ActionStart
)

func BuildCurrentParentRequestCompletionProjection(cfg config.AppConfig, st *state.StateStore) (ParentRequestCompletionProjection, error) {
	status := st.TaskStatus()
	if status == state.TaskStatusAwaitingParentCompletion || status == state.TaskStatusComplete {
		return BuildParentRequestCompletionProjection(cfg)
	}
	output, err := buildProjectState(cfg, st)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return parentRequestProjection(output.Continuation), nil
}

func BuildParentRequestCompletionProjection(cfg config.AppConfig) (ParentRequestCompletionProjection, error) {
	projection, err := repositoryprojecttree.BuildParentRequestCompletionProjection(cfg.RepoRoot)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return parentRequestProjectionFromPolicy(projection), nil
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
