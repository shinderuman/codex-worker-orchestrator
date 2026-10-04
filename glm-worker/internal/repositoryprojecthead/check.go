package repositoryprojecthead

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"

func CheckFinalHeadPlan(root string) (string, error) {
	snapshot, err := finalHeadPlan(root)
	if err != nil {
		return "", err
	}
	if !snapshot.Present {
		return "plan final head: " + snapshot.Status, nil
	}
	prepared, err := repositoryproject.PrepareFinalHead(snapshot.Plan)
	if err != nil {
		return "", err
	}
	if err := validatePreparedPlan(root, snapshot.Head, prepared); err != nil {
		return "", err
	}
	return "plan final head: verified", nil
}
