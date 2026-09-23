package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool                              `json:"completion_admitted"`
	StopAdmitted       bool                              `json:"stop_admitted"`
	Continuation       projectContinuationProjection     `json:"continuation"`
	TaskAttribution    repositoryproject.TaskAttribution `json:"task_attribution"`
}

func BuildCurrentParentRequestCompletionProjection(cfg config.AppConfig, st *state.StateStore) (ParentRequestCompletionProjection, error) {
	request, err := parentcontinuation.BuildCurrentRequest(cfg.RepoRoot, st)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return parentRequestProjectionFromFocused(request), nil
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

func parentRequestProjectionFromFocused(request parentcontinuation.Request) ParentRequestCompletionProjection {
	projection := ParentRequestCompletionProjection{
		CompletionAdmitted: request.CompletionAdmitted,
		StopAdmitted:       request.StopAdmitted,
		Continuation:       projectContinuationFromPolicy(request.Continuation),
		TaskAttribution:    request.TaskAttribution,
	}
	if request.Automation != nil {
		projection.Continuation.Automation = &projectContinuationAutomation{
			AutomationID: request.Automation.AutomationID,
			ParentThread: request.Automation.ParentThread,
			WakeThread:   request.Automation.WakeThread,
			ResumeAtUTC:  request.Automation.ResumeAtUTC,
			WakeAtUTC:    request.Automation.WakeAtUTC,
		}
	}
	return projection
}

func parentRequestProjectionFromPolicy(projection repositoryproject.ParentRequestCompletionProjection) ParentRequestCompletionProjection {
	return ParentRequestCompletionProjection{
		CompletionAdmitted: projection.CompletionAdmitted,
		StopAdmitted:       projection.StopAdmitted,
		Continuation:       projectContinuationFromPolicy(projection.Continuation),
	}
}
