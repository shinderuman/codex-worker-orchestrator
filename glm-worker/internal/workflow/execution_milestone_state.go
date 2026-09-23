package workflow

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) initializeExecutionMilestones(definitions []ExecutionMilestoneDefinition, activeTaskPath string) error {
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	digest, err := executionmilestone.TaskContractDigest(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return err
	}
	plan := executionmilestone.NewPlan(taskID, activeTaskPath, digest, definitions, w.now().UTC())
	return executionmilestone.Save(w.state, plan)
}

func (w *Workflow) completeCurrentExecutionMilestone(plan *executionMilestonePlan, result packet.Result) error {
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	current := &plan.Milestones[plan.CurrentIndex]
	current.Status = executionmilestone.StatusComplete
	current.Completion = &executionmilestone.Completion{
		CompletedAt:        w.now().UTC(),
		CallID:             w.lastCallID,
		WorkerSessionID:    w.state.ReadOr("worker.id", ""),
		Summary:            result.Summary,
		TaskContractSHA256: plan.TaskContractSHA256,
		Snapshot:           snapshot,
	}
	plan.CurrentIndex++
	plan.UpdatedAt = w.now().UTC()
	return executionmilestone.Save(w.state, plan)
}

func ReviseExecutionMilestones(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []ExecutionMilestoneDefinition,
	now time.Time,
) (ExecutionMilestoneRevision, error) {
	return executionmilestone.Revise(cfg, st, definitions, now)
}

func executionTaskContractDigest(repoRoot, activeTaskPath string) (string, error) {
	return executionmilestone.TaskContractDigest(repoRoot, activeTaskPath)
}

func loadExecutionMilestonePlan(st *state.StateStore) (*executionMilestonePlan, error) {
	return executionmilestone.Load(st)
}

func saveExecutionMilestonePlan(st *state.StateStore, plan *executionMilestonePlan) error {
	return executionmilestone.Save(st, plan)
}
