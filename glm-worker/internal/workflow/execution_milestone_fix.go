package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ExecuteExplicitFixWithExecutionMilestones(instruction, origin, cause, acceptedScope string) error {
	active, err := w.hasPendingExecutionMilestone()
	if err != nil {
		return err
	}
	if !active {
		return quietWhenParentFileGuardStopped(w.withTemp(func() error {
			return w.executeExplicitFixWithAcceptedScopeLifecycle(instruction, origin, cause, acceptedScope)
		}))
	}
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		return w.executeExecutionMilestoneExplicitFix(instruction, origin, cause, acceptedScope)
	}))
}

func (w *Workflow) executeExplicitFixWithAcceptedScopeLifecycle(instruction, origin, cause, acceptedScope string) error {
	if err := w.admitParentAction(state.ParentActionFix); err != nil {
		return err
	}
	request, err := w.state.Read("last-request")
	if err != nil {
		return &WorkerError{Message: "no previous task for this repository"}
	}
	if err := w.prepareAcceptedFixScopeChecked(acceptedScope); err != nil {
		return err
	}

	decision := w.state.ReadOr("last-decision", "none")
	review := w.state.ReadOr("last-review", "none")
	rollback, err := w.state.BeginParentFix(origin, cause)
	if err != nil {
		return w.discardAcceptedFixScopeAfterFailure(err)
	}

	activeTaskPath, err := w.ensureActiveTaskPath("worker-explicit-fix")
	if err != nil {
		return w.rollbackParentFixAndAcceptedScope(rollback, err)
	}
	decl, err := w.gateExternalFeasibility("worker-explicit-fix", false)
	if err != nil {
		return w.rollbackParentFixAndAcceptedScope(rollback, err)
	}
	pocStage := decl.pocStage()
	prompt := explicitFixPrompt(request, decision, review, instruction, activeTaskPath)
	checkpoint := state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-explicit-fix",
		Role:           state.WorkerRole,
		Model:          w.config.WorkerModel,
		ReadOnly:       pocStage,
		Effort:         w.config.EscalatedEffort,
		Prompt:         prompt,
		OriginalPrompt: prompt,
		Request:        request,
		Decision:       decision,
	}
	return w.rollbackParentFixScopeWhenPreCallGuardFailure(
		rollback,
		w.executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath, checkpoint, pocStage),
	)
}

func (w *Workflow) executeExecutionMilestoneExplicitFix(instruction, origin, cause, acceptedScope string) error {
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
	if err := w.prepareAcceptedFixScopeChecked(acceptedScope); err != nil {
		return err
	}
	decision := w.state.ReadOr("last-decision", "none")
	review := w.state.ReadOr("last-review", "none")
	rollback, err := w.state.BeginParentFix(origin, cause)
	if err != nil {
		return w.discardAcceptedFixScopeAfterFailure(err)
	}
	prompt := explicitFixPrompt(request, decision, review, instruction, activeTaskPath)
	contextBlock, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, 1)
	if err != nil {
		return w.rollbackParentFixAndAcceptedScope(rollback, err)
	}
	prompt += contextBlock
	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-explicit-fix", Role: state.WorkerRole,
		Model: w.config.WorkerModel, ReadOnly: decl.pocStage(), Effort: w.config.EscalatedEffort,
		Prompt: prompt, OriginalPrompt: prompt, Request: request, Decision: decision,
	}
	return w.rollbackParentFixScopeWhenPreCallGuardFailure(
		rollback,
		w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, decl.pocStage()),
	)
}

func (w *Workflow) discardAcceptedFixScopeAfterFailure(cause error) error {
	if scopeErr := w.discardPreparedAcceptedFixScope(); scopeErr != nil {
		return fmt.Errorf("%w; accepted fix scope rollback failed: %w", cause, scopeErr)
	}
	return cause
}

func (w *Workflow) rollbackParentFixAndAcceptedScope(rollback state.ParentActionRollback, cause error) error {
	scopeErr := w.discardPreparedAcceptedFixScope()
	rollbackErr := w.state.RollbackParentAction(rollback, cause)
	if scopeErr != nil {
		return fmt.Errorf("%w; accepted fix scope rollback failed: %w", rollbackErr, scopeErr)
	}
	return rollbackErr
}

func (w *Workflow) rollbackParentFixScopeWhenPreCallGuardFailure(rollback state.ParentActionRollback, err error) error {
	if err == nil || w.modelCallAttempts != 1 || !runner.IsPreCallGuardFailure(err) {
		return err
	}
	return w.rollbackParentFixAndAcceptedScope(rollback, err)
}
