package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ExecuteNewTaskWithMilestones(request string, definitions []ExecutionMilestoneDefinition) error {
	if err := validateExecutionMilestoneDefinitions(definitions); err != nil {
		return err
	}
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		return w.executeNewTaskWithMilestones(request, definitions)
	}))
}

func (w *Workflow) executeNewTaskWithMilestones(request string, definitions []ExecutionMilestoneDefinition) error {
	if err := w.validateNewTaskStart(); err != nil {
		return err
	}
	activeTaskPath, err := w.initializeNewTask(request)
	if err != nil {
		return err
	}
	if err := w.initializeExecutionMilestones(definitions, activeTaskPath); err != nil {
		return err
	}
	decl, err := w.gateExternalFeasibility("worker-new", false)
	if err != nil {
		return err
	}
	checkpoint, err := w.newExecutionMilestoneCheckpoint(
		request,
		activeTaskPath,
		"worker-new",
		w.config.RoutineEffort,
		decl.pocStage(),
		1,
	)
	if err != nil {
		return err
	}
	return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, decl.pocStage())
}

func (w *Workflow) ExecuteDecisionWithExecutionMilestones(decision string) error {
	active, err := w.hasPendingExecutionMilestone()
	if err != nil {
		return err
	}
	if !active {
		return w.ExecuteDecision(decision)
	}
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		return w.executeExecutionMilestoneDecision(decision)
	}))
}

func (w *Workflow) executeExecutionMilestoneDecision(decision string) error {
	context, err := w.prepareExecutionMilestoneDecision(decision)
	if err != nil {
		return err
	}
	checkpoint, err := w.executionMilestoneDecisionCheckpoint(context, decision)
	if err != nil {
		return err
	}
	return w.executeExecutionMilestoneWorkerCheckpoint(context.request, checkpoint, context.pocStage)
}

func (w *Workflow) prepareExecutionMilestoneDecision(decision string) (executionMilestoneDecisionContext, error) {
	if w.state.TaskStatus() != state.TaskStatusWaitingDecision || !w.state.Exists("pending-decision") {
		return executionMilestoneDecisionContext{}, &WorkerError{Message: "no pending Sol decision for this repository"}
	}
	request, err := w.state.Read("last-request")
	if err != nil {
		return executionMilestoneDecisionContext{}, &WorkerError{Message: "original request is missing"}
	}
	activeTaskPath, err := w.gateDecisionActiveTask()
	if err != nil {
		return executionMilestoneDecisionContext{}, err
	}
	decl, err := w.gateExternalFeasibility("worker-decision", true)
	if err != nil {
		return executionMilestoneDecisionContext{}, err
	}
	if err := w.replaceAcceptedScopeWithDecision(decision); err != nil {
		return executionMilestoneDecisionContext{}, err
	}
	if err := w.state.BeginParentDecision(); err != nil {
		return executionMilestoneDecisionContext{}, err
	}
	return executionMilestoneDecisionContext{
		request: request, activeTaskPath: activeTaskPath, pocStage: decl.pocStage(),
	}, nil
}

func (w *Workflow) executionMilestoneDecisionCheckpoint(
	context executionMilestoneDecisionContext,
	decision string,
) (state.ResumeCheckpoint, error) {
	prompt := decisionPrompt(context.request, decision, context.activeTaskPath)
	searchContext, err := w.exhaustiveSearchContext(context.request, context.activeTaskPath, state.WorkerRole, 1)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	prompt += searchContext
	return state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-decision", Role: state.WorkerRole,
		Model: w.config.WorkerModel, ReadOnly: context.pocStage, Effort: w.config.EscalatedEffort,
		Prompt: prompt, OriginalPrompt: prompt, Request: context.request, Decision: decision,
	}, nil
}

func (w *Workflow) ExecuteResumeWithExecutionMilestones() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil || checkpoint.ExecutionMilestoneID == "" {
		return w.ExecuteResume()
	}
	return quietWhenParentFileGuardStopped(w.withTemp(w.executeExecutionMilestoneResume))
}

func (w *Workflow) executeExecutionMilestoneResume() error {
	checkpoint, decl, pocResume, err := w.loadResumeCheckpoint()
	if err != nil {
		return err
	}
	if err := w.validateExecutionMilestoneCheckpointAuthority(checkpoint); err != nil {
		return err
	}
	reuseCompletedResult, err := w.prepareGuardRecovery(checkpoint)
	if err != nil {
		return err
	}
	previousCheckpoint := checkpoint
	completedResult := checkpoint.CompletedResult
	checkpoint, stopped, err := w.prepareResumeCheckpoint(checkpoint, decl, pocResume)
	if err != nil || stopped {
		return err
	}
	checkpoint, err = w.decorateExecutionMilestoneCheckpoint(checkpoint)
	if err != nil {
		return err
	}
	clearGuardRecoveryState(&checkpoint)
	if reuseCompletedResult && completedResult != nil {
		if err := w.state.ClearResumeCheckpoint(); err != nil {
			return err
		}
		return w.routeExecutionMilestoneResumeResult(checkpoint, decl, *completedResult)
	}
	w.resetInstructionReadObservation()
	result, err := w.runModel(checkpoint)
	if err != nil {
		return w.handleResumeRunError(checkpoint, previousCheckpoint, err)
	}
	return w.routeExecutionMilestoneResumeResult(checkpoint, decl, result)
}

func (w *Workflow) routeExecutionMilestoneResumeResult(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	result packet.Result,
) error {
	if checkpoint.Stage != state.ResumeStageWorker {
		return w.routeResumeResult(checkpoint, decl, result)
	}
	if decl.pocStage() {
		if stopped, err := w.verifyPoCEndSnapshot(); err != nil || stopped {
			return err
		}
		if result.Status == packet.StatusImplemented {
			return w.routePoCWorkerResult(result)
		}
	}
	result, err := w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
	if err != nil {
		return err
	}
	return w.handleExecutionMilestoneWorkerResult(checkpoint.Request, result, checkpoint)
}

func (w *Workflow) ExecuteQualitySurfaceApprovalWithExecutionMilestones(acceptedScope string) error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil || checkpoint.ExecutionMilestoneID == "" {
		return w.ExecuteQualitySurfaceApproval(acceptedScope)
	}
	if err := w.validateExecutionMilestoneCheckpointAuthority(checkpoint); err != nil {
		return err
	}
	return w.withTemp(func() error {
		return w.executeExecutionMilestoneQualitySurfaceApproval(acceptedScope)
	})
}

func (w *Workflow) executeExecutionMilestoneQualitySurfaceApproval(acceptedScope string) error {
	if acceptedScope != acceptedFixScopeCurrentDiff {
		return &WorkerError{Message: "quality-surface approval requires accepted scope current-diff"}
	}
	if w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
		return &WorkerError{Message: "quality-surface approval is only available while waiting for Sol review"}
	}
	w.prepareAcceptedFixScope(acceptedScope)
	checkpoint, handled, err := w.loadApprovedQualitySurfaceCheckpoint()
	if err != nil {
		return err
	}
	if !handled {
		return &WorkerError{Message: "no retained quality-surface approval checkpoint is available"}
	}
	return w.resumeApprovedExecutionMilestoneQualitySurface(checkpoint)
}

func (w *Workflow) resumeApprovedExecutionMilestoneQualitySurface(checkpoint state.ResumeCheckpoint) error {
	result := *checkpoint.CompletedResult
	if err := w.activateApprovedQualitySurface(); err != nil {
		return err
	}
	var err error
	result, err = w.convergeWorkerRuleActivation(checkpoint, result, checkpointActivatedRules(checkpoint))
	if err != nil {
		return err
	}
	if checkpoint.Stage != state.ResumeStageWorker {
		return w.routeApprovedQualitySurface(checkpoint, result)
	}
	return w.handleExecutionMilestoneWorkerResult(checkpoint.Request, result, checkpoint)
}

func (w *Workflow) executeExecutionMilestoneWorkerCheckpoint(
	request string,
	checkpoint state.ResumeCheckpoint,
	pocStage bool,
) error {
	decorated, err := w.decorateExecutionMilestoneCheckpoint(checkpoint)
	if err != nil {
		return err
	}
	checkpoint = decorated
	if pocStage {
		stopped, err := w.savePoCStartSnapshot()
		if err != nil || stopped {
			return err
		}
	}
	workerResult, err := w.runWorkerModelWithRuleActivation(checkpoint)
	if err != nil {
		return err
	}
	if pocStage {
		stopped, err := w.verifyPoCEndSnapshot()
		if err != nil || stopped {
			return err
		}
		if workerResult.Status == packet.StatusImplemented {
			return w.routePoCWorkerResult(workerResult)
		}
	}
	return w.handleExecutionMilestoneWorkerResult(request, workerResult, checkpoint)
}

func (w *Workflow) handleExecutionMilestoneWorkerResult(
	request string,
	workerResult packet.Result,
	checkpoint state.ResumeCheckpoint,
) error {
	if stopped, err := w.verifyQualitySurfaceBaseline(checkpoint.Phase); err != nil || stopped {
		return err
	}
	switch workerResult.Status {
	case packet.StatusNeedsSolDecision:
		return w.stopExecutionMilestoneForDecision(workerResult)
	case packet.StatusImplemented:
		return w.acceptExecutionMilestoneWorkerResult(request, workerResult, checkpoint)
	default:
		return &WorkerError{Phase: "worker-format", Message: "worker did not return a valid STATUS"}
	}
}

func (w *Workflow) stopExecutionMilestoneForDecision(result packet.Result) error {
	if err := w.state.WaitForDecision(); err != nil {
		return err
	}
	return w.emitResult(result)
}

func (w *Workflow) acceptExecutionMilestoneWorkerResult(
	request string,
	workerResult packet.Result,
	checkpoint state.ResumeCheckpoint,
) error {
	if err := w.state.ContinueAfterWorkerResult(); err != nil {
		return err
	}
	advanced, err := w.advanceExecutionMilestone(request, workerResult, checkpoint)
	if err != nil || advanced {
		return err
	}
	return w.reviewUntilStable(request, workerResult, 1, 0, checkpoint.Phase)
}

func (w *Workflow) newExecutionMilestoneCheckpoint(
	request string,
	activeTaskPath string,
	phase string,
	effort string,
	readOnly bool,
	seq int,
) (state.ResumeCheckpoint, error) {
	prompt := w.newWorkerTaskPrompt(request, activeTaskPath)
	contextBlock, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, seq)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	prompt += contextBlock
	return state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: phase, Role: state.WorkerRole,
		Model: w.config.WorkerModel, ReadOnly: readOnly, Effort: effort,
		Prompt: prompt, OriginalPrompt: prompt, Request: request,
	}, nil
}

func (w *Workflow) decorateExecutionMilestoneCheckpoint(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil {
		return checkpoint, err
	}
	if plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return checkpoint, fmt.Errorf("no pending execution milestone is available")
	}
	if err := w.validateExecutionMilestoneAuthority(plan); err != nil {
		return checkpoint, err
	}
	current := plan.Milestones[plan.CurrentIndex]
	if checkpoint.ExecutionMilestoneID != "" && checkpoint.ExecutionMilestoneID != current.ID {
		return checkpoint, fmt.Errorf("execution milestone checkpoint mismatch: checkpoint=%q current=%q", checkpoint.ExecutionMilestoneID, current.ID)
	}
	checkpoint.ExecutionMilestoneID = current.ID
	block, err := executionMilestonePromptBlock(plan)
	if err != nil {
		return checkpoint, err
	}
	checkpoint.Prompt = replaceExecutionMilestonePromptBlock(checkpoint.Prompt, block)
	if checkpoint.OriginalPrompt != "" {
		checkpoint.OriginalPrompt = replaceExecutionMilestonePromptBlock(checkpoint.OriginalPrompt, block)
	}
	return checkpoint, nil
}

func (w *Workflow) advanceExecutionMilestone(
	request string,
	result packet.Result,
	checkpoint state.ResumeCheckpoint,
) (bool, error) {
	if checkpoint.ExecutionMilestoneID == "" {
		return false, fmt.Errorf("execution milestone result has no checkpoint identity")
	}
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil {
		return false, err
	}
	if err := w.validateExecutionMilestoneAuthority(plan); err != nil {
		return false, err
	}
	if err := validateCurrentExecutionMilestone(plan, checkpoint.ExecutionMilestoneID); err != nil {
		return false, err
	}
	if err := w.completeCurrentExecutionMilestone(plan, result); err != nil {
		return false, err
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return false, nil
	}
	return true, w.startNextExecutionMilestone(request, checkpoint, plan)
}

func (w *Workflow) startNextExecutionMilestone(
	request string,
	previous state.ResumeCheckpoint,
	plan *executionMilestonePlan,
) error {
	current := plan.Milestones[plan.CurrentIndex]
	if current.FreshWorker {
		if err := w.state.InvalidateSession(state.WorkerRole); err != nil {
			return err
		}
	}
	phase := fmt.Sprintf("worker-milestone-%d", plan.CurrentIndex+1)
	decl, err := w.gateExternalFeasibility(phase, false)
	if err != nil {
		return err
	}
	effort := previous.Effort
	if effort == "" {
		effort = w.config.RoutineEffort
	}
	checkpoint, err := w.newExecutionMilestoneCheckpoint(
		request,
		plan.ActiveTaskPath,
		phase,
		effort,
		decl.pocStage(),
		plan.CurrentIndex+1,
	)
	if err != nil {
		return err
	}
	checkpoint.Decision = previous.Decision
	return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, decl.pocStage())
}

func (w *Workflow) hasPendingExecutionMilestone() (bool, error) {
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil || plan == nil {
		return false, err
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return false, nil
	}
	return true, w.validateExecutionMilestoneAuthority(plan)
}

func (w *Workflow) validateExecutionMilestoneCheckpointAuthority(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.ExecutionMilestoneID == "" {
		return &WorkerError{Phase: checkpoint.Phase, Message: "execution milestone checkpoint has no milestone identity"}
	}
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil {
		return err
	}
	if err := w.validateExecutionMilestoneAuthority(plan); err != nil {
		return err
	}
	return validateCurrentExecutionMilestone(plan, checkpoint.ExecutionMilestoneID)
}

func (w *Workflow) validateExecutionMilestoneAuthority(plan *executionMilestonePlan) error {
	if plan == nil {
		return fmt.Errorf("execution milestone plan is missing")
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	if taskID != plan.TaskID {
		return fmt.Errorf("execution milestone task identity changed: plan=%q current=%q", plan.TaskID, taskID)
	}
	activeTaskPath := w.state.ReadOr(activeTaskStateKey, "")
	if activeTaskPath != plan.ActiveTaskPath {
		return fmt.Errorf("execution milestone ACTIVE task changed: plan=%q current=%q", plan.ActiveTaskPath, activeTaskPath)
	}
	digest, err := executionTaskContractDigest(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return err
	}
	if digest != plan.TaskContractSHA256 {
		return fmt.Errorf("execution milestone task contract changed; revise milestones at the parent boundary before continuing")
	}
	return nil
}

func validateCurrentExecutionMilestone(plan *executionMilestonePlan, milestoneID string) error {
	if plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("execution milestone %q has no active durable plan", milestoneID)
	}
	current := plan.Milestones[plan.CurrentIndex]
	if current.ID != milestoneID || current.Status != executionMilestonePending {
		return fmt.Errorf("execution milestone state mismatch: checkpoint=%q current=%q status=%q", milestoneID, current.ID, current.Status)
	}
	return nil
}
