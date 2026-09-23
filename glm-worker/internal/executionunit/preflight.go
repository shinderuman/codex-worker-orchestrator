package executionunit

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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type MilestoneDefinition struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Acceptance  string `json:"acceptance"`
	FreshWorker bool   `json:"fresh_worker,omitempty"`
}

type Decision struct {
	Decision      string
	ExecutionUnit string
	Milestones    []MilestoneDefinition
}

type milestoneInput struct {
	Milestones []MilestoneDefinition `json:"milestones"`
}

type milestoneRecord struct {
	MilestoneDefinition
}

type milestonePlan struct {
	Version            int               `json:"version"`
	TaskID             string            `json:"task_id"`
	ActiveTaskPath     string            `json:"active_task_path"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	CurrentIndex       int               `json:"current_index"`
	Milestones         []milestoneRecord `json:"milestones"`
}

const (
	ExecutionUnitSingle     = "single"
	ExecutionUnitMilestones = "milestones"

	executionUnitPrefix  = "EXECUTION_UNIT: "
	milestonesJSONPrefix = "MILESTONES_JSON: "
	decisionMarker       = "DECISION:"

	milestonePlanVersion  = 1
	milestoneMaxCount     = 8
	milestoneMaxIDBytes   = 64
	milestoneMaxTextBytes = 2048
	activeTaskStateKey    = "active-task"
)

func IsPayload(payload string) bool {
	return strings.HasPrefix(payload, executionUnitPrefix)
}

func Parse(payload string) (Decision, error) {
	parts := strings.SplitN(payload, "\n", 4)
	if len(parts) != 4 || !strings.HasPrefix(parts[0], executionUnitPrefix) ||
		!strings.HasPrefix(parts[1], milestonesJSONPrefix) || parts[2] != decisionMarker {
		return Decision{}, fmt.Errorf("decision payload must use the machine-owned execution-unit template")
	}

	input := Decision{
		ExecutionUnit: strings.TrimSpace(strings.TrimPrefix(parts[0], executionUnitPrefix)),
		Decision:      strings.TrimSpace(parts[3]),
	}
	if input.Decision == "" {
		return Decision{}, fmt.Errorf("decision payload is empty")
	}

	milestones, err := parseMilestones(parts[1])
	if err != nil {
		return Decision{}, err
	}
	input.Milestones = milestones
	if err := validateDecision(input); err != nil {
		return Decision{}, err
	}
	return input, nil
}

func Preflight(cfg config.AppConfig, st *state.StateStore, payload string) (Decision, bool, error) {
	input, err := Parse(payload)
	if err != nil {
		return Decision{}, false, err
	}
	active, err := hasPendingMilestone(cfg, st)
	if err != nil {
		return Decision{}, false, err
	}

	switch input.ExecutionUnit {
	case ExecutionUnitSingle:
		if active {
			return Decision{}, false, fmt.Errorf("execution-unit single cannot bypass pending execution milestones")
		}
	case ExecutionUnitMilestones:
		if len(input.Milestones) == 0 {
			if !active {
				return Decision{}, false, fmt.Errorf("execution-unit milestones requires 2-8 milestone definitions or an existing pending milestone plan")
			}
			return input, active, nil
		}
		if err := validateMilestoneRevision(cfg, st, input.Milestones); err != nil {
			return Decision{}, false, err
		}
	default:
		return Decision{}, false, fmt.Errorf("unsupported execution-unit disposition %q", input.ExecutionUnit)
	}
	return input, active, nil
}

func parseMilestones(line string) ([]MilestoneDefinition, error) {
	var input milestoneInput
	payload := strings.TrimSpace(strings.TrimPrefix(line, milestonesJSONPrefix))
	if err := decodeMilestoneJSON(payload, &input); err != nil {
		return nil, err
	}
	return input.Milestones, nil
}

func validateDecision(input Decision) error {
	switch input.ExecutionUnit {
	case ExecutionUnitSingle:
		if len(input.Milestones) != 0 {
			return fmt.Errorf("execution-unit single cannot include milestone definitions")
		}
	case ExecutionUnitMilestones:
		if len(input.Milestones) > 0 {
			return validateMilestoneDefinitions(input.Milestones)
		}
	default:
		return fmt.Errorf("execution unit must be %q or %q", ExecutionUnitSingle, ExecutionUnitMilestones)
	}
	return nil
}

func decodeMilestoneJSON(payload string, target any) error {
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

func validateMilestoneDefinitions(definitions []MilestoneDefinition) error {
	if len(definitions) < 2 || len(definitions) > milestoneMaxCount {
		return fmt.Errorf("execution milestones require 2-%d entries", milestoneMaxCount)
	}
	seen := make(map[string]struct{}, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Scope = strings.TrimSpace(definition.Scope)
		definition.Acceptance = strings.TrimSpace(definition.Acceptance)
		if err := validateMilestoneDefinition(*definition, seen); err != nil {
			return err
		}
		seen[definition.ID] = struct{}{}
	}
	return nil
}

func validateMilestoneDefinition(definition MilestoneDefinition, seen map[string]struct{}) error {
	if definition.ID == "" || len(definition.ID) > milestoneMaxIDBytes {
		return fmt.Errorf("execution milestone id must be 1-%d bytes", milestoneMaxIDBytes)
	}
	if _, exists := seen[definition.ID]; exists {
		return fmt.Errorf("duplicate execution milestone id %q", definition.ID)
	}
	if definition.Scope == "" || len(definition.Scope) > milestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q scope must be 1-%d bytes", definition.ID, milestoneMaxTextBytes)
	}
	if definition.Acceptance == "" || len(definition.Acceptance) > milestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q acceptance must be 1-%d bytes", definition.ID, milestoneMaxTextBytes)
	}
	return nil
}

func hasPendingMilestone(cfg config.AppConfig, st *state.StateStore) (bool, error) {
	plan, err := loadMilestonePlan(st)
	if err != nil || plan == nil {
		return false, err
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return false, nil
	}
	if err := validateMilestoneAuthority(cfg, st, plan); err != nil {
		return true, err
	}
	return true, nil
}

func validateMilestoneAuthority(cfg config.AppConfig, st *state.StateStore, plan *milestonePlan) error {
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	if taskID != plan.TaskID {
		return fmt.Errorf("execution milestone task identity changed: plan=%q current=%q", plan.TaskID, taskID)
	}
	activeTaskPath := st.ReadOr(activeTaskStateKey, "")
	if activeTaskPath != plan.ActiveTaskPath {
		return fmt.Errorf("execution milestone ACTIVE task changed: plan=%q current=%q", plan.ActiveTaskPath, activeTaskPath)
	}
	digest, err := taskContractDigest(cfg.RepoRoot, activeTaskPath)
	if err != nil {
		return err
	}
	if digest != plan.TaskContractSHA256 {
		return fmt.Errorf("execution milestone task contract changed; revise milestones at the parent boundary before continuing")
	}
	return nil
}

func validateMilestoneRevision(cfg config.AppConfig, st *state.StateStore, definitions []MilestoneDefinition) error {
	if err := validateMilestoneDefinitions(definitions); err != nil {
		return err
	}
	if !milestoneRevisionStatusAllowed(st.TaskStatus()) {
		return fmt.Errorf("execution milestones can only be revised at a stopped worker parent boundary")
	}
	plan, err := loadMilestonePlan(st)
	if err != nil {
		return err
	}
	return validateMilestoneRevisionAgainstPlan(cfg, st, definitions, plan)
}

func validateMilestoneRevisionAgainstPlan(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []MilestoneDefinition,
	plan *milestonePlan,
) error {
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	activeTaskPath := st.ReadOr(activeTaskStateKey, "")
	if _, err := taskContractDigest(cfg.RepoRoot, activeTaskPath); err != nil {
		return err
	}
	if plan == nil {
		return nil
	}
	if plan.TaskID != taskID || plan.ActiveTaskPath != activeTaskPath {
		return fmt.Errorf("execution milestone plan does not belong to the active task")
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return fmt.Errorf("all execution milestones are already complete")
	}
	if len(definitions) <= plan.CurrentIndex {
		return fmt.Errorf("revised execution milestones must preserve all completed milestones and one current milestone")
	}
	if err := validateCompletedMilestones(plan, definitions); err != nil {
		return err
	}
	return validateStoppedMilestone(st, plan, definitions)
}

func validateCompletedMilestones(plan *milestonePlan, definitions []MilestoneDefinition) error {
	for index := 0; index < plan.CurrentIndex; index++ {
		if plan.Milestones[index].MilestoneDefinition != definitions[index] {
			return fmt.Errorf("completed execution milestone %q is immutable", plan.Milestones[index].ID)
		}
	}
	return nil
}

func validateStoppedMilestone(st *state.StateStore, plan *milestonePlan, definitions []MilestoneDefinition) error {
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

func milestoneRevisionStatusAllowed(status state.TaskStatus) bool {
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

func loadMilestonePlan(st *state.StateStore) (*milestonePlan, error) {
	data, err := os.ReadFile(st.Path(state.ExecutionMilestonesStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plan milestonePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("read execution milestone state: %w", err)
	}
	if plan.Version != milestonePlanVersion {
		return nil, fmt.Errorf("unsupported execution milestone state version: %d", plan.Version)
	}
	if plan.CurrentIndex < 0 || plan.CurrentIndex > len(plan.Milestones) {
		return nil, fmt.Errorf("invalid execution milestone current index: %d", plan.CurrentIndex)
	}
	return &plan, nil
}

func taskContractDigest(repoRoot, activeTaskPath string) (string, error) {
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
