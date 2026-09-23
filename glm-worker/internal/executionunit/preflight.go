package executionunit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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

type taskPlanInput struct {
	Request    string                `json:"request"`
	Milestones []MilestoneDefinition `json:"milestones"`
}

const (
	ExecutionUnitSingle     = "single"
	ExecutionUnitMilestones = "milestones"

	executionUnitPrefix  = "EXECUTION_UNIT: "
	milestonesJSONPrefix = "MILESTONES_JSON: "
	decisionMarker       = "DECISION:"

	milestoneMaxCount     = 8
	milestoneMaxIDBytes   = 64
	milestoneMaxTextBytes = 2048
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

	milestones, err := ParseMilestonePayload(strings.TrimSpace(strings.TrimPrefix(parts[1], milestonesJSONPrefix)))
	if err != nil {
		return Decision{}, err
	}
	input.Milestones = milestones
	if err := validateDecision(input); err != nil {
		return Decision{}, err
	}
	return input, nil
}

func ParseTaskPlanPayload(payload string) (string, []MilestoneDefinition, error) {
	var input taskPlanInput
	if err := decodeJSON(payload, &input); err != nil {
		return "", nil, err
	}
	input.Request = strings.TrimSpace(input.Request)
	if input.Request == "" {
		return "", nil, fmt.Errorf("execution milestone task request is required")
	}
	if err := ValidateMilestoneDefinitions(input.Milestones); err != nil {
		return "", nil, err
	}
	return input.Request, input.Milestones, nil
}

func ParseMilestonePayload(payload string) ([]MilestoneDefinition, error) {
	var input milestoneInput
	if err := decodeJSON(payload, &input); err != nil {
		return nil, err
	}
	if len(input.Milestones) > 0 {
		if err := ValidateMilestoneDefinitions(input.Milestones); err != nil {
			return nil, err
		}
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
			return ValidateMilestoneDefinitions(input.Milestones)
		}
	default:
		return fmt.Errorf("execution unit must be %q or %q", ExecutionUnitSingle, ExecutionUnitMilestones)
	}
	return nil
}

func decodeJSON(payload string, target any) error {
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

func ValidateMilestoneDefinitions(definitions []MilestoneDefinition) error {
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
