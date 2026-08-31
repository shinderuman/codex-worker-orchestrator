package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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
		records[index] = executionMilestoneRecord{
			ExecutionMilestoneDefinition: definition,
			Status:                       executionMilestonePending,
		}
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

func (w *Workflow) completeCurrentExecutionMilestone(plan *executionMilestonePlan, result packet.Result) error {
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("capture execution milestone completion snapshot: %w", err)
	}
	current := &plan.Milestones[plan.CurrentIndex]
	current.Status = executionMilestoneComplete
	current.Completion = &executionMilestoneCompletion{
		CompletedAt:        w.now().UTC(),
		CallID:             w.lastCallID,
		WorkerSessionID:    w.state.ReadOr("worker.id", ""),
		Summary:            result.Summary,
		TaskContractSHA256: plan.TaskContractSHA256,
		Snapshot:           snapshot,
	}
	plan.CurrentIndex++
	plan.UpdatedAt = w.now().UTC()
	return saveExecutionMilestonePlan(w.state, plan)
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
	case state.TaskStatusWaitingDecision,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusInterrupted:
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
	if err := validateCompletedExecutionMilestoneDefinitions(plan, definitions); err != nil {
		return err
	}
	if err := preserveStoppedExecutionMilestone(st, plan, definitions); err != nil {
		return err
	}
	plan.Milestones = revisedExecutionMilestoneRecords(plan, definitions)
	return nil
}

func validateCompletedExecutionMilestoneDefinitions(
	plan *executionMilestonePlan,
	definitions []ExecutionMilestoneDefinition,
) error {
	for index := 0; index < plan.CurrentIndex; index++ {
		current := plan.Milestones[index].ExecutionMilestoneDefinition
		if !sameExecutionMilestoneDefinition(current, definitions[index]) {
			return fmt.Errorf("completed execution milestone %q is immutable", plan.Milestones[index].ID)
		}
	}
	return nil
}

func revisedExecutionMilestoneRecords(
	plan *executionMilestonePlan,
	definitions []ExecutionMilestoneDefinition,
) []executionMilestoneRecord {
	records := append([]executionMilestoneRecord(nil), plan.Milestones[:plan.CurrentIndex]...)
	for _, definition := range definitions[plan.CurrentIndex:] {
		records = append(records, executionMilestoneRecord{
			ExecutionMilestoneDefinition: definition,
			Status:                       executionMilestonePending,
		})
	}
	return records
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
		Status:         "updated",
		TaskID:         plan.TaskID,
		CurrentIndex:   plan.CurrentIndex,
		MilestoneCount: len(plan.Milestones),
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
