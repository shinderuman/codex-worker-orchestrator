package workflow

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecthead"

func CheckFinalHeadPlan(root string) (string, error) {
	return repositoryprojecthead.CheckFinalHeadPlan(root)
}

func CheckParentCompletionHead(root string) (string, error) {
	return repositoryprojecthead.CheckParentCompletionHead(root)
}
