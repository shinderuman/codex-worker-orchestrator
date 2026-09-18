package repositoryprojecttree

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func BuildTaskAttribution(repoRoot, lifecycleTask string, continuation repositoryproject.Continuation) (repositoryproject.TaskAttribution, error) {
	planContent, err := ReadPlan(repoRoot)
	if err != nil {
		return repositoryproject.TaskAttribution{}, err
	}
	if planContent == nil {
		return repositoryproject.DeriveTaskAttribution(lifecycleTask, "", continuation), nil
	}
	schedule := taskcontract.ParsePlanSchedule(*planContent)
	if len(schedule.Active) == 0 {
		return repositoryproject.DeriveTaskAttribution(lifecycleTask, "", continuation), nil
	}
	activeTask, err := schedule.ActiveTask()
	if err != nil {
		return repositoryproject.TaskAttribution{}, err
	}
	return repositoryproject.DeriveTaskAttribution(lifecycleTask, activeTask, continuation), nil
}
