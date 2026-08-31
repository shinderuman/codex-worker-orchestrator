package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ExecutionMilestoneDefinition struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Acceptance  string `json:"acceptance"`
	FreshWorker bool   `json:"fresh_worker,omitempty"`
}

type ExecutionMilestoneRevision struct {
	Status         string `json:"status"`
	TaskID         string `json:"task_id"`
	CurrentIndex   int    `json:"current_index"`
	MilestoneCount int    `json:"milestone_count"`
	CurrentID      string `json:"current_id,omitempty"`
}

type executionTaskPlanInput struct {
	Request    string                         `json:"request"`
	Milestones []ExecutionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneInput struct {
	Milestones []ExecutionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneCompletion struct {
	CompletedAt        time.Time         `json:"completed_at"`
	CallID             string            `json:"call_id,omitempty"`
	WorkerSessionID    string            `json:"worker_session_id,omitempty"`
	Summary            string            `json:"summary"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	Snapshot           state.GitSnapshot `json:"snapshot"`
}

type executionMilestoneRecord struct {
	ExecutionMilestoneDefinition
	Status     string                        `json:"status"`
	Completion *executionMilestoneCompletion `json:"completion,omitempty"`
}

type executionMilestonePlan struct {
	Version            int                        `json:"version"`
	TaskID             string                     `json:"task_id"`
	ActiveTaskPath     string                     `json:"active_task_path"`
	TaskContractSHA256 string                     `json:"task_contract_sha256"`
	CurrentIndex       int                        `json:"current_index"`
	Milestones         []executionMilestoneRecord `json:"milestones"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}

type executionMilestonePrompt struct {
	TaskAuthority      string                              `json:"task_authority"`
	TaskContractSHA256 string                              `json:"task_contract_sha256"`
	Current            ExecutionMilestoneDefinition        `json:"current"`
	Completed          []executionMilestoneCompletedPrompt `json:"completed,omitempty"`
}

type executionMilestoneCompletedPrompt struct {
	ID                 string            `json:"id"`
	Summary            string            `json:"summary"`
	CallID             string            `json:"call_id,omitempty"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	Snapshot           state.GitSnapshot `json:"snapshot"`
}

const (
	executionMilestonePlanVersion = 1
	executionMilestonePending      = "pending"
	executionMilestoneComplete     = "complete"
	executionMilestoneMaxCount     = 8
	executionMilestoneMaxIDBytes   = 64
	executionMilestoneMaxTextBytes = 2048

	executionMilestonePromptBegin = "BEGIN_EXECUTION_MILESTONE_JSON"
	executionMilestonePromptEnd   = "END_EXECUTION_MILESTONE_JSON"
)

func ParseExecutionTaskPlanPayload(payload string) (string, []ExecutionMilestoneDefinition, error) {
	var input executionTaskPlanInput
	if err := decodeExecutionMilestoneJSON(payload, &input); err != nil {
		return "", nil, err
	}
	input.Request = strings.TrimSpace(input.Request)
	if input.Request == "" {
		return "", nil, fmt.Errorf("execution milestone task request is required")
	}
	if err := validateExecutionMilestoneDefinitions(input.Milestones); err != nil {
		return "", nil, err
	}
	return input.Request, input.Milestones, nil
}

func ParseExecutionMilestonePayload(payload string) ([]ExecutionMilestoneDefinition, error) {
	var input executionMilestoneInput
	if err := decodeExecutionMilestoneJSON(payload, &input); err != nil {
		return nil, err
	}
	if err := validateExecutionMilestoneDefinitions(input.Milestones); err != nil {
		return nil, err
	}
	return input.Milestones, nil
}

func decodeExecutionMilestoneJSON(payload string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("execution milestone payload is invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("execution milestone payload has trailing JSON")
	}
	return nil
}

func validateExecutionMilestoneDefinitions(definitions []ExecutionMilestoneDefinition) error {
	if len(definitions) < 2 || len(definitions) > executionMilestoneMaxCount {
		return fmt.Errorf("execution milestones require 2-%d entries", executionMilestoneMaxCount)
	}
	seen := make(map[string]struct{}, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Scope = strings.TrimSpace(definition.Scope)
		definition.Acceptance = strings.TrimSpace(definition.Acceptance)
		if err := validateExecutionMilestoneDefinition(*definition, seen); err != nil {
			return err
		}
		seen[definition.ID] = struct{}{}
	}
	return nil
}

func validateExecutionMilestoneDefinition(definition ExecutionMilestoneDefinition, seen map[string]struct{}) error {
	if definition.ID == "" || len(definition.ID) > executionMilestoneMaxIDBytes {
		return fmt.Errorf("execution milestone id must be 1-%d bytes", executionMilestoneMaxIDBytes)
	}
	if _, exists := seen[definition.ID]; exists {
		return fmt.Errorf("duplicate execution milestone id %q", definition.ID)
	}
	if definition.Scope == "" || len(definition.Scope) > executionMilestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q scope must be 1-%d bytes", definition.ID, executionMilestoneMaxTextBytes)
	}
	if definition.Acceptance == "" || len(definition.Acceptance) > executionMilestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q acceptance must be 1-%d bytes", definition.ID, executionMilestoneMaxTextBytes)
	}
	return nil
}

func (w *Workflow) ExecuteNewTaskWithMilestones(request string, definitions []ExecutionMilestoneDefinition) error {
	if err := validateExecutionMilestoneDefinitions(definitions); err != nil {
		return err
	}
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
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
		pocStage := decl.pocStage()
		checkpoint, err := w.newExecutionMilestoneCheckpoint(request, activeTaskPath, "worker-new", w.config.RoutineEffort, pocStage, 1)
		if err != nil {
			return err
		}
		return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, pocStage)
	}))
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
		if w.state.TaskStatus() != state.TaskStatusWaitingDecision || !w.state.Exists("pending-decision") {
			return &WorkerError{Message: "no pending Sol decision for this repository"}
		}
		request, err := w.state.Read("last-request")
		if err != nil {
			return &WorkerError{Message: "original request is missing"}
		}
		activeTaskPath, err := w.gateDecisionActiveTask()
		if err != nil {
			return err
		}
		decl, err := w.gateExternalFeasibility("worker-decision", true)
		if err != nil {
			return err
		}
		pocStage := decl.pocStage()
		if err := w.replaceAcceptedScopeWithDecision(decision); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		w.state.RecordDecision()
		if _, err := w.state.RecordParentOutcome(state.ParentOutcomeDecision, ""); err != nil {
			return err
		}
		prompt := decisionPrompt(request, decision, activeTaskPath)
		checkpoint := state.ResumeCheckpoint{
			Stage: state.ResumeStageWorker, Phase: "worker-decision", Role: state.WorkerRole,
			Model: w.config.WorkerModel, ReadOnly: pocStage, Effort: w.config.EscalatedEffort,
			Prompt: prompt, OriginalPrompt: prompt, Request: request, Decision: decision,
		}
		contextBlock, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, 1)
		if err != nil {
			return err
		}
		checkpoint.Prompt += contextBlock
		checkpoint.OriginalPrompt = checkpoint.Prompt
		return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, pocStage)
	}))
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
	if checkpoint.ExecutionMilestoneID == "" {
		return &WorkerError{Phase: checkpoint.Phase, Message: "execution milestone resume lost milestone identity"}
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
	return w.withTemp(func() error {
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
		result := *checkpoint.CompletedResult
		if err := w.activateApprovedQualitySurface(); err != nil {
			return err
		}
		result, err = w.convergeWorkerRuleActivation(checkpoint, result, checkpointActivatedRules(checkpoint))
		if err != nil {
			return err
		}
		if checkpoint.Stage != state.ResumeStageWorker {
			return w.routeApprovedQualitySurface(checkpoint, result)
		}
		return w.handleExecutionMilestoneWorkerResult(checkpoint.Request, result, checkpoint)
	})
}

func (w *Workflow) executeExecutionMilestoneWorkerCheckpoint(
	request string,
	checkpoint state.ResumeCheckpoint,
	pocStage bool,
) error {
	var err error
	checkpoint, err = w.decorateExecutionMilestoneCheckpoint(checkpoint)
	if err != nil {
		return err
	}
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
		if err := w.state.Touch("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			return err
		}
		return w.emitResult(workerResult)
	case packet.StatusImplemented:
		if err := w.state.Remove("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		advanced, err := w.advanceExecutionMilestone(request, workerResult, checkpoint)
		if err != nil || advanced {
			return err
		}
		return w.reviewUntilStable(request, workerResult, 1, 0, checkpoint.Phase)
	default:
		return &WorkerError{Phase: "worker-format", Message: "worker did not return a valid STATUS"}
	}
}

func (w *Workflow) initializeExecutionMilestones(definitions []ExecutionMilestoneDefinition, activeTaskPath string) error {
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	digest, err := executionTaskContractDigest(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return err
	}
	plan := newExecutionMilestonePlan(taskID, activeTaskPath, digest, definitions, w.now().UTC())
	return saveExecutionMilestonePlan(w.state, plan)
}

func newExecutionMilestonePlan(
	taskID string,
	activeTaskPath string,
	digest string,
	definitions []ExecutionMilestoneDefinition,
	now time.Time,
) *executionMilestonePlan {
	records := make([]executionMilestoneRecord, len(definitions))
	for index, definition := range definitions {
		records[index] = executionMilestoneRecord{ExecutionMilestoneDefinition: definition, Status: executionMilestonePending}
	}
	return &executionMilestonePlan{
		Version: executionMilestonePlanVersion, TaskID: taskID, ActiveTaskPath: activeTaskPath,
		TaskContractSHA256: digest, Milestones: records, UpdatedAt: now,
	}
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

func executionMilestonePromptBlock(plan *executionMilestonePlan) (string, error) {
	prompt := executionMilestonePrompt{
		TaskAuthority: plan.ActiveTaskPath, TaskContractSHA256: plan.TaskContractSHA256,
		Current: plan.Milestones[plan.CurrentIndex].ExecutionMilestoneDefinition,
	}
	for _, record := range plan.Milestones[:plan.CurrentIndex] {
		if record.Status != executionMilestoneComplete || record.Completion == nil {
			return "", fmt.Errorf("completed execution milestone %q has no completion evidence", record.ID)
		}
		prompt.Completed = append(prompt.Completed, executionMilestoneCompletedPrompt{
			ID: record.ID, Summary: record.Completion.Summary, CallID: record.Completion.CallID,
			TaskContractSHA256: record.Completion.TaskContractSHA256, Snapshot: record.Completion.Snapshot,
		})
	}
	data, err := json.Marshal(prompt)
	if err != nil {
		return "", fmt.Errorf("encode execution milestone prompt: %w", err)
	}
	return "\n\n" + executionMilestonePromptBegin + "\n" + string(data) + "\n" +
		"ACTIVE task remains task-wide authority; implement only current.scope, satisfy current.acceptance, and do not reimplement completed milestones. Milestone completion never completes or weakens task-wide Acceptance.\n" +
		executionMilestonePromptEnd + "\n", nil
}

func replaceExecutionMilestonePromptBlock(prompt, block string) string {
	for {
		start := strings.Index(prompt, executionMilestonePromptBegin)
		if start < 0 {
			break
		}
		if prefix := strings.LastIndex(prompt[:start], "\n\n"); prefix >= 0 {
			start = prefix
		}
		endRelative := strings.Index(prompt[start:], executionMilestonePromptEnd)
		if endRelative < 0 {
			break
		}
		end := start + endRelative + len(executionMilestonePromptEnd)
		for end < len(prompt) && prompt[end] == '\n' {
			end++
		}
		prompt = prompt[:start] + prompt[end:]
	}
	return strings.TrimRight(prompt, "\n") + block
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
		if err := w.state.Remove("worker.id", "worker.ready"); err != nil {
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
		request, plan.ActiveTaskPath, phase, effort, decl.pocStage(), plan.CurrentIndex+1,
	)
	if err != nil {
		return err
	}
	checkpoint.Decision = previous.Decision
	return w.executeExecutionMilestoneWorkerCheckpoint(request, checkpoint, decl.pocStage())
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

func (w *Workflow) completeCurrentExecutionMilestone(plan *executionMilestonePlan, result packet.Result) error {
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("capture execution milestone completion snapshot: %w", err)
	}
	current := &plan.Milestones[plan.CurrentIndex]
	current.Status = executionMilestoneComplete
	current.Completion = &executionMilestoneCompletion{
		CompletedAt: w.now().UTC(), CallID: w.lastCallID,
		WorkerSessionID: w.state.ReadOr("worker.id", ""), Summary: result.Summary,
		TaskContractSHA256: plan.TaskContractSHA256, Snapshot: snapshot,
	}
	plan.CurrentIndex++
	plan.UpdatedAt = w.now().UTC()
	return saveExecutionMilestonePlan(w.state, plan)
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

func ReviseExecutionMilestones(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []ExecutionMilestoneDefinition,
) (ExecutionMilestoneRevision, error) {
	if err := validateExecutionMilestoneDefinitions(definitions); err != nil {
		return ExecutionMilestoneRevision{}, err
	}
	if !executionMilestoneRevisionStatusAllowed(st.TaskStatus()) {
		return ExecutionMilestoneRevision{}, fmt.Errorf("execution milestones can only be revised at a stopped worker parent boundary")
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		return ExecutionMilestoneRevision{}, err
	}
	plan, err = prepareExecutionMilestoneRevision(cfg, st, plan, definitions)
	if err != nil {
		return ExecutionMilestoneRevision{}, err
	}
	if err := saveExecutionMilestonePlan(st, plan); err != nil {
		return ExecutionMilestoneRevision{}, err
	}
	if err := bindStoppedCheckpointToExecutionMilestone(st, plan); err != nil {
		return ExecutionMilestoneRevision{}, err
	}
	return executionMilestoneRevisionResult(plan), nil
}

func executionMilestoneRevisionStatusAllowed(status state.TaskStatus) bool {
	switch status {
	case state.TaskStatusWaitingDecision, state.TaskStatusRateLimited, state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable, state.TaskStatusInterrupted:
		return true
	default:
		return false
	}
}

func prepareExecutionMilestoneRevision(
	cfg config.AppConfig,
	st *state.StateStore,
	plan *executionMilestonePlan,
	definitions []ExecutionMilestoneDefinition,
) (*executionMilestonePlan, error) {
	taskID, err := st.TaskID()
	if err != nil {
		return nil, err
	}
	activeTaskPath := st.ReadOr(activeTaskStateKey, "")
	digest, err := executionTaskContractDigest(cfg.RepoRoot, activeTaskPath)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return newExecutionMilestonePlan(taskID, activeTaskPath, digest, definitions, time.Now().UTC()), nil
	}
	if plan.TaskID != taskID || plan.ActiveTaskPath != activeTaskPath {
		return nil, fmt.Errorf("execution milestone plan does not belong to the active task")
	}
	if err := reconcileExecutionMilestoneDefinitions(st, plan, definitions); err != nil {
		return nil, err
	}
	plan.TaskContractSHA256 = digest
	plan.UpdatedAt = time.Now().UTC()
	return plan, nil
}

func reconcileExecutionMilestoneDefinitions(
	st *state.StateStore,
	plan *executionMilestonePlan,
	definitions []ExecutionMilestoneDefinition,
) error {
	if plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("all execution milestones are already complete")
	}
	if len(definitions) <= plan.CurrentIndex {
		return fmt.Errorf("revised execution milestones must preserve all completed milestones and one current milestone")
	}
	for index := 0; index < plan.CurrentIndex; index++ {
		if !sameExecutionMilestoneDefinition(plan.Milestones[index].ExecutionMilestoneDefinition, definitions[index]) {
			return fmt.Errorf("completed execution milestone %q is immutable", plan.Milestones[index].ID)
		}
	}
	if err := preserveStoppedExecutionMilestone(st, plan, definitions); err != nil {
		return err
	}
	records := append([]executionMilestoneRecord(nil), plan.Milestones[:plan.CurrentIndex]...)
	for _, definition := range definitions[plan.CurrentIndex:] {
		records = append(records, executionMilestoneRecord{ExecutionMilestoneDefinition: definition, Status: executionMilestonePending})
	}
	plan.Milestones = records
	return nil
}

func preserveStoppedExecutionMilestone(
	st *state.StateStore,
	plan *executionMilestonePlan,
	definitions []ExecutionMilestoneDefinition,
) error {
	checkpoint, err := st.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	if checkpoint.ExecutionMilestoneID == "" {
		return nil
	}
	current := plan.Milestones[plan.CurrentIndex].ExecutionMilestoneDefinition
	if checkpoint.ExecutionMilestoneID != current.ID || !sameExecutionMilestoneDefinition(current, definitions[plan.CurrentIndex]) {
		return fmt.Errorf("cannot revise the stopped in-flight execution milestone %q", checkpoint.ExecutionMilestoneID)
	}
	return nil
}

func bindStoppedCheckpointToExecutionMilestone(st *state.StateStore, plan *executionMilestonePlan) error {
	if plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return nil
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	currentID := plan.Milestones[plan.CurrentIndex].ID
	if checkpoint.ExecutionMilestoneID != "" && checkpoint.ExecutionMilestoneID != currentID {
		return fmt.Errorf("stopped checkpoint belongs to execution milestone %q, not %q", checkpoint.ExecutionMilestoneID, currentID)
	}
	checkpoint.ExecutionMilestoneID = currentID
	return st.SaveResumeCheckpoint(checkpoint)
}

func sameExecutionMilestoneDefinition(left, right ExecutionMilestoneDefinition) bool {
	return left == right
}

func executionMilestoneRevisionResult(plan *executionMilestonePlan) ExecutionMilestoneRevision {
	result := ExecutionMilestoneRevision{
		Status: "updated", TaskID: plan.TaskID, CurrentIndex: plan.CurrentIndex, MilestoneCount: len(plan.Milestones),
	}
	if plan.CurrentIndex < len(plan.Milestones) {
		result.CurrentID = plan.Milestones[plan.CurrentIndex].ID
	}
	return result
}

func executionTaskContractDigest(repoRoot, activeTaskPath string) (string, error) {
	if strings.TrimSpace(activeTaskPath) == "" {
		return "", fmt.Errorf("execution milestones require a Plan-selected ACTIVE task")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(activeTaskPath)))
	if err != nil {
		return "", fmt.Errorf("read execution milestone ACTIVE task: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func loadExecutionMilestonePlan(st *state.StateStore) (*executionMilestonePlan, error) {
	data, err := os.ReadFile(st.Path(state.ExecutionMilestonesStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plan executionMilestonePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("read execution milestone state: %w", err)
	}
	if plan.Version != executionMilestonePlanVersion {
		return nil, fmt.Errorf("unsupported execution milestone state version: %d", plan.Version)
	}
	if plan.CurrentIndex < 0 || plan.CurrentIndex > len(plan.Milestones) {
		return nil, fmt.Errorf("invalid execution milestone current index: %d", plan.CurrentIndex)
	}
	return &plan, nil
}

func saveExecutionMilestonePlan(st *state.StateStore, plan *executionMilestonePlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode execution milestone state: %w", err)
	}
	return st.Write(state.ExecutionMilestonesStateFile, string(data))
}

var _ runner.RunResult
