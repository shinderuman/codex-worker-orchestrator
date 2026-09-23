package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func (w *Workflow) initializeExecutionMilestones(definitions []executionunit.MilestoneDefinition, activeTaskPath string) error {
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

func (w *Workflow) completeCurrentExecutionMilestone(plan *executionmilestone.Plan, result packet.Result) error {
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("capture execution milestone completion snapshot: %w", err)
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
