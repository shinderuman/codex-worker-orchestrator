package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool                              `json:"completion_admitted"`
	StopAdmitted       bool                              `json:"stop_admitted"`
	Continuation       projectContinuationProjection     `json:"continuation"`
	TaskAttribution    repositoryproject.TaskAttribution `json:"task_attribution"`
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
