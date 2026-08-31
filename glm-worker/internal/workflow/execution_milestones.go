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
	CompletedAt     time.Time         `json:"completed_at"`
	CallID          string            `json:"call_id,omitempty"`
	WorkerSessionID string            `json:"worker_session_id,omitempty"`
	Summary         string            `json:"summary"`
	Snapshot        state.GitSnapshot `json:"snapshot"`
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
	TaskAuthority      string                               `json:"task_authority"`
	TaskContractSHA256 string                               `json:"task_contract_sha256"`
	Current            ExecutionMilestoneDefinition         `json:"current"`
	Completed          []executionMilestoneCompletedPrompt  `json:"completed,omitempty"`
}

type executionMilestoneCompletedPrompt struct {
	ID       string            `json:"id"`
	Summary  string            `json:"summary"`
	CallID   string            `json:"call_id,omitempty"`
	Snapshot state.GitSnapshot `json:"snapshot"`
}

const (
	executionMilestonePlanVersion = 1
	executionMilestonePending      = "pending"
	executionMilestoneComplete     = "complete"
	executionMilestoneMaxCount     = 8
	executionMilestoneMaxIDBytes   = 64
	executionMilestoneMaxTextBytes = 2048
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

func (w *Workflow) initializeExecutionMilestones(definitions []ExecutionMilestoneDefinition, activeTaskPath string) error {
	if len(definitions) == 0 {
		return nil
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	digest, err := executionTaskContractDigest(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return err
	}
	plan := newExecutionMilestonePlan(taskID, activeTaskPath, digest, definitions, w.now())
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
		Version:            executionMilestonePlanVersion,
		TaskID:             taskID,
		ActiveTaskPath:     activeTaskPath,
		TaskContractSHA256: digest,
		Milestones:         records,
		UpdatedAt:          now,
	}
}

func (w *Workflow) decorateExecutionMilestoneCheckpoint(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil || plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return checkpoint, err
	}
	current := plan.Milestones[plan.CurrentIndex]
	checkpoint.ExecutionMilestoneID = current.ID
	block, err := executionMilestonePromptBlock(plan)
	if err != nil {
		return checkpoint, err
	}
	checkpoint.Prompt = strings.TrimRight(checkpoint.Prompt, "\n") + block
	if checkpoint.OriginalPrompt != "" {
		checkpoint.OriginalPrompt = strings.TrimRight(checkpoint.OriginalPrompt, "\n") + block
	}
	return checkpoint, nil
}

func executionMilestonePromptBlock(plan *executionMilestonePlan) (string, error) {
	prompt := executionMilestonePrompt{
		TaskAuthority:      plan.ActiveTaskPath,
		TaskContractSHA256: plan.TaskContractSHA256,
		Current:            plan.Milestones[plan.CurrentIndex].ExecutionMilestoneDefinition,
	}
	for _, record := range plan.Milestones[:plan.CurrentIndex] {
		if record.Completion == nil {
			return "", fmt.Errorf("completed execution milestone %q has no completion evidence", record.ID)
		}
		prompt.Completed = append(prompt.Completed, executionMilestoneCompletedPrompt{
			ID: record.ID, Summary: record.Completion.Summary,
			CallID: record.Completion.CallID, Snapshot: record.Completion.Snapshot,
		})
	}
	data, err := json.Marshal(prompt)
	if err != nil {
		return "", fmt.Errorf("encode execution milestone prompt: %w", err)
	}
	return "\n\nEXECUTION_MILESTONE_JSON:\n" + string(data) +
		"\nACTIVE task remains task-wide authority; complete only the current milestone and do not reimplement completed milestones.\n", nil
}

func (w *Workflow) advanceExecutionMilestone(
	request string,
	result packet.Result,
	checkpoint state.ResumeCheckpoint,
) (bool, error) {
	if checkpoint.ExecutionMilestoneID == "" {
		return false, nil
	}
	plan, err := loadExecutionMilestonePlan(w.state)
	if err != nil {
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
	if plan.Milestones[plan.CurrentIndex].FreshWorker {
		if err := w.state.Remove("worker.id", "worker.ready"); err != nil {
			return false, err
		}
	}
	next := w.nextExecutionMilestoneCheckpoint(request, checkpoint, plan)
	return true, w.executeWorkerCheckpoint(request, next, false)
}

func validateCurrentExecutionMilestone(plan *executionMilestonePlan, milestoneID string) error {
	if plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("execution milestone %q has no active durable plan", milestoneID)
	}
	if current := plan.Milestones[plan.CurrentIndex]; current.ID != milestoneID || current.Status != executionMilestonePending {
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
		WorkerSessionID: w.state.ReadOr("worker.id", ""), Summary: result.Summary, Snapshot: snapshot,
	}
	plan.CurrentIndex++
	plan.UpdatedAt = w.now().UTC()
	return saveExecutionMilestonePlan(w.state, plan)
}

func (w *Workflow) nextExecutionMilestoneCheckpoint(
	request string,
	previous state.ResumeCheckpoint,
	plan *executionMilestonePlan,
) state.ResumeCheckpoint {
	prompt := newTaskPrompt(request, plan.ActiveTaskPath)
	return state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: fmt.Sprintf("worker-milestone-%d", plan.CurrentIndex+1),
		Role: state.WorkerRole, Model: w.config.WorkerModel, Effort: previous.Effort,
		Prompt: prompt, OriginalPrompt: prompt, Request: request, Decision: previous.Decision,
	}
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
	if errors.Is(err, state.ErrNoResumeCheckpoint) || checkpoint.ExecutionMilestoneID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	current := plan.Milestones[plan.CurrentIndex].ExecutionMilestoneDefinition
	if checkpoint.ExecutionMilestoneID != current.ID || !sameExecutionMilestoneDefinition(current, definitions[plan.CurrentIndex]) {
		return fmt.Errorf("cannot revise the stopped in-flight execution milestone %q", checkpoint.ExecutionMilestoneID)
	}
	return nil
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
	return &plan, nil
}

func saveExecutionMilestonePlan(st *state.StateStore, plan *executionMilestonePlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode execution milestone state: %w", err)
	}
	return st.Write(state.ExecutionMilestonesStateFile, string(data))
}
