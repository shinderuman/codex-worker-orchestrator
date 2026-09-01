package workflow

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ExecuteExplicitFixWithExecutionMilestones(instruction, origin, acceptedScope string) error {
	active, err := w.hasPendingExecutionMilestone()
	if err != nil {
		return err
	}
	if !active {
		return w.ExecuteExplicitFixWithScope(instruction, origin, acceptedScope)
	}
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		return w.executeExecutionMilestoneExplicitFix(instruction, origin, acceptedScope)
	}))
}

func (w *Workflow) executeExecutionMilestoneExplicitFix(instruction, origin, acceptedScope string) error {
	if err := w.admitParentAction(state.ParentActionFix); err != nil {
		return err
	}
	request, err := w.state.Read("last-request")
	if err != nil {
		return &WorkerError{Message: "no previous task for this repository"}
	}
	activeTaskPath, err := w.ensureActiveTaskPath("worker-explicit-fix")
	if err != nil {
		return err
	}
	decl, err := w.gateExternalFeasibility("worker-explicit-fix", true)
	if err != nil {
		return err
	}
	w.prepareAcceptedFixScope(acceptedScope)
	decision := w.state.ReadOr("last-decision", "none")
	review := w.state.ReadOr("last-review", "none")
	if err := w.state.BeginParentFix(origin); err != nil {
		return err
	}
	prompt := explicitFixPrompt(request, decision, review, instruction, activeTaskPath)
	contextBlock, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, 1)
	if err != nil {
		return err
	}
	prompt += contextBlock
	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-explicit-fix", Role: state.WorkerRole,
		Model: w.config.WorkerModel, ReadOnly: decl.pocStage(), Effort: w.config.EscalatedEffort,
		Prompt: prompt, OriginalPrompt: prompt, Request: request, Decision: decision,
	}
	return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, decl.pocStage())
}
