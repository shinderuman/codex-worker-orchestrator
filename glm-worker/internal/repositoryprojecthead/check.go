package repositoryprojecthead

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func CheckFinalHeadPlan(root string) (string, error) {
	snapshot, err := finalHeadPlan(root)
	if err != nil {
		return "", err
	}
	if !snapshot.Present {
		return "plan final head: " + snapshot.Status, nil
	}
	return checkPlanSnapshot(root, snapshot)
}

func CheckCommitPlan(root, commit string) (string, error) {
	head, err := gitOutput(root, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return "", err
	}
	plan, err := gitOutput(root, "show", head+":"+state.ParentPlanFile)
	if err != nil {
		return "", fmt.Errorf("candidate Planを読めません: %w", err)
	}
	return checkPlanSnapshot(root, planSnapshot{Plan: plan, Head: head, Present: true})
}

func checkPlanSnapshot(root string, snapshot planSnapshot) (string, error) {
	prepared, err := repositoryproject.PrepareFinalHead(snapshot.Plan)
	if err != nil {
		return "", err
	}
	if err := validatePreparedPlan(root, snapshot.Head, prepared); err != nil {
		return "", err
	}
	return "plan final head: verified", nil
}
