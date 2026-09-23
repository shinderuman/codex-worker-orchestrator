package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"

type projectContinuationProjection struct {
	repositoryproject.Continuation
	Automation *projectContinuationAutomation `json:"automation,omitempty"`
}

func projectContinuationFromPolicy(continuation repositoryproject.Continuation) projectContinuationProjection {
	return projectContinuationProjection{Continuation: continuation}
}

func unknownProjectContinuation(reason string) projectContinuationProjection {
	return projectContinuationFromPolicy(repositoryproject.UnknownContinuation(reason))
}
