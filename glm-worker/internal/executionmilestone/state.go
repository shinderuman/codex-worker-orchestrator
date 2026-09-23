package executionmilestone

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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Completion struct {
	CompletedAt        time.Time         `json:"completed_at"`
	CallID             string            `json:"call_id,omitempty"`
	WorkerSessionID    string            `json:"worker_session_id,omitempty"`
	Summary            string            `json:"summary"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	Snapshot           state.GitSnapshot `json:"snapshot"`
}

type Record struct {
	executionunit.MilestoneDefinition
	Status     string      `json:"status"`
	Completion *Completion `json:"completion,omitempty"`
}

type Plan struct {
	Version            int       `json:"version"`
	TaskID             string    `json:"task_id"`
	ActiveTaskPath     string    `json:"active_task_path"`
	TaskContractSHA256 string    `json:"task_contract_sha256"`
	CurrentIndex       int       `json:"current_index"`
	Milestones         []Record  `json:"milestones"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Revision struct {
	Status         string `json:"status"`
	TaskID         string `json:"task_id"`
	CurrentIndex   int    `json:"current_index"`
	MilestoneCount int    `json:"milestone_count"`
	CurrentID      string `json:"current_id,omitempty"`
}

type revisionAuthority struct {
	taskID         string
	activeTaskPath string
	digest         string
}

const (
	PlanVersion    = 1
	StatusPending  = "pending"
	StatusComplete = "complete"

	activeTaskStateKey = "active-task"
)

func NewPlan(
	taskID string,
	activeTaskPath string,
	digest string,
	definitions []executionunit.MilestoneDefinition,
	now time.Time,
) *Plan {
	records := make([]Record, len(definitions))
	for index, definition := range definitions {
		records[index] = Record{
			MilestoneDefinition: definition,
			Status:              StatusPending,
		}
	}
	return &Plan{
		Version:            PlanVersion,
		TaskID:             taskID,
		ActiveTaskPath:     activeTaskPath,
		TaskContractSHA256: digest,
		Milestones:         records,
		UpdatedAt:          now,
	}
}

func Load(st *state.StateStore) (*Plan, error) {
	data, err := os.ReadFile(st.Path(state.ExecutionMilestonesStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("read execution milestone state: %w", err)
	}
	if plan.Version != PlanVersion {
		return nil, fmt.Errorf("unsupported execution milestone state version: %d", plan.Version)
	}
	if plan.CurrentIndex < 0 || plan.CurrentIndex > len(plan.Milestones) {
		return nil, fmt.Errorf("invalid execution milestone current index: %d", plan.CurrentIndex)
	}
	return &plan, nil
}

func Save(st *state.StateStore, plan *Plan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode execution milestone state: %w", err)
	}
	return st.Write(state.ExecutionMilestonesStateFile, string(data))
}

func TaskContractDigest(repoRoot, activeTaskPath string) (string, error) {
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

func ValidateAuthority(cfg config.AppConfig, st *state.StateStore, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("execution milestone plan is missing")
	}
	authority, err := currentAuthority(cfg, st)
	if err != nil {
		return err
	}
	if authority.taskID != plan.TaskID {
		return fmt.Errorf("execution milestone task identity changed: plan=%q current=%q", plan.TaskID, authority.taskID)
	}
	if authority.activeTaskPath != plan.ActiveTaskPath {
		return fmt.Errorf("execution milestone ACTIVE task changed: plan=%q current=%q", plan.ActiveTaskPath, authority.activeTaskPath)
	}
	if authority.digest != plan.TaskContractSHA256 {
		return fmt.Errorf("execution milestone task contract changed; revise milestones at the parent boundary before continuing")
	}
	return nil
}

func HasPending(cfg config.AppConfig, st *state.StateStore) (bool, error) {
	plan, err := Load(st)
	if err != nil || plan == nil {
		return false, err
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return false, nil
	}
	if err := ValidateAuthority(cfg, st, plan); err != nil {
		return true, err
	}
	return true, nil
}

func Preflight(cfg config.AppConfig, st *state.StateStore, input executionunit.Decision) (bool, error) {
	active, err := HasPending(cfg, st)
	if err != nil {
		return false, err
	}

	switch input.ExecutionUnit {
	case executionunit.ExecutionUnitSingle:
		if active {
			return false, fmt.Errorf("execution-unit single cannot bypass pending execution milestones")
		}
	case executionunit.ExecutionUnitMilestones:
		if len(input.Milestones) == 0 {
			if !active {
				return false, fmt.Errorf("execution-unit milestones requires 2-8 milestone definitions or an existing pending milestone plan")
			}
			return active, nil
		}
		if err := ValidateRevision(cfg, st, input.Milestones); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported execution-unit disposition %q", input.ExecutionUnit)
	}
	return active, nil
}

func ValidateRevision(cfg config.AppConfig, st *state.StateStore, definitions []executionunit.MilestoneDefinition) error {
	_, err := prepareRevision(cfg, st, definitions, time.Time{})
	return err
}

func Revise(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []executionunit.MilestoneDefinition,
	now time.Time,
) (Revision, error) {
	plan, err := prepareRevision(cfg, st, definitions, now.UTC())
	if err != nil {
		return Revision{}, err
	}
	if err := bindStoppedCheckpoint(st, plan); err != nil {
		return Revision{}, err
	}
	if err := Save(st, plan); err != nil {
		return Revision{}, err
	}
	return revisionResult(plan), nil
}

func prepareRevision(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []executionunit.MilestoneDefinition,
	now time.Time,
) (*Plan, error) {
	if err := executionunit.ValidateMilestoneDefinitions(definitions); err != nil {
		return nil, err
	}
	if !revisionStatusAllowed(st.TaskStatus()) {
		return nil, fmt.Errorf("execution milestones can only be revised at a stopped worker parent boundary")
	}
	plan, err := Load(st)
	if err != nil {
		return nil, err
	}
	authority, err := currentAuthority(cfg, st)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return NewPlan(authority.taskID, authority.activeTaskPath, authority.digest, definitions, now), nil
	}
	if err := validateRevisionPlan(st, plan, authority, definitions); err != nil {
		return nil, err
	}
	plan.Milestones = revisedRecords(plan, definitions)
	plan.TaskContractSHA256 = authority.digest
	if !now.IsZero() {
		plan.UpdatedAt = now
	}
	return plan, nil
}

func currentAuthority(cfg config.AppConfig, st *state.StateStore) (revisionAuthority, error) {
	taskID, err := st.TaskID()
	if err != nil {
		return revisionAuthority{}, err
	}
	activeTaskPath := st.ReadOr(activeTaskStateKey, "")
	digest, err := TaskContractDigest(cfg.RepoRoot, activeTaskPath)
	if err != nil {
		return revisionAuthority{}, err
	}
	return revisionAuthority{taskID: taskID, activeTaskPath: activeTaskPath, digest: digest}, nil
}

func validateRevisionPlan(
	st *state.StateStore,
	plan *Plan,
	authority revisionAuthority,
	definitions []executionunit.MilestoneDefinition,
) error {
	if plan.TaskID != authority.taskID || plan.ActiveTaskPath != authority.activeTaskPath {
		return fmt.Errorf("execution milestone plan does not belong to the active task")
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("all execution milestones are already complete")
	}
	if len(definitions) <= plan.CurrentIndex {
		return fmt.Errorf("revised execution milestones must preserve all completed milestones and one current milestone")
	}
	if err := validateCompleted(plan, definitions); err != nil {
		return err
	}
	return preserveStopped(st, plan, definitions)
}

func validateCompleted(plan *Plan, definitions []executionunit.MilestoneDefinition) error {
	for index := 0; index < plan.CurrentIndex; index++ {
		if plan.Milestones[index].MilestoneDefinition != definitions[index] {
			return fmt.Errorf("completed execution milestone %q is immutable", plan.Milestones[index].ID)
		}
	}
	return nil
}

func preserveStopped(st *state.StateStore, plan *Plan, definitions []executionunit.MilestoneDefinition) error {
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
	current := plan.Milestones[plan.CurrentIndex].MilestoneDefinition
	if checkpoint.ExecutionMilestoneID != current.ID || current != definitions[plan.CurrentIndex] {
		return fmt.Errorf("cannot revise the stopped in-flight execution milestone %q", checkpoint.ExecutionMilestoneID)
	}
	return nil
}

func revisedRecords(plan *Plan, definitions []executionunit.MilestoneDefinition) []Record {
	records := append([]Record(nil), plan.Milestones[:plan.CurrentIndex]...)
	for _, definition := range definitions[plan.CurrentIndex:] {
		records = append(records, Record{
			MilestoneDefinition: definition,
			Status:              StatusPending,
		})
	}
	return records
}

func bindStoppedCheckpoint(st *state.StateStore, plan *Plan) error {
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

func ValidateCurrent(plan *Plan, milestoneID string) error {
	if plan == nil || plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("execution milestone %q has no active durable plan", milestoneID)
	}
	current := plan.Milestones[plan.CurrentIndex]
	if current.ID != milestoneID || current.Status != StatusPending {
		return fmt.Errorf("execution milestone state mismatch: checkpoint=%q current=%q status=%q", milestoneID, current.ID, current.Status)
	}
	return nil
}

func revisionStatusAllowed(status state.TaskStatus) bool {
	switch status {
	case state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusQualityGateRecoverable,
		state.TaskStatusInterrupted:
		return true
	default:
		return false
	}
}

func revisionResult(plan *Plan) Revision {
	result := Revision{
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
