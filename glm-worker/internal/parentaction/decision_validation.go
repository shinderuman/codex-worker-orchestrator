package parentaction

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type stagedDecisionMilestoneDefinition struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Acceptance  string `json:"acceptance"`
	FreshWorker bool   `json:"fresh_worker,omitempty"`
}

type stagedDecisionMilestoneInput struct {
	Milestones []stagedDecisionMilestoneDefinition `json:"milestones"`
}

const (
	stagedDecisionExecutionUnitSingle     = "single"
	stagedDecisionExecutionUnitMilestones = "milestones"
	stagedDecisionMilestoneMaxCount       = 8
	stagedDecisionMilestoneMaxIDBytes     = 64
	stagedDecisionMilestoneMaxTextBytes   = 2048
)

func validateDecisionPayloadSemantics(parts [][]byte) error {
	executionUnit := strings.TrimSpace(strings.TrimPrefix(string(parts[0]), decisionExecutionUnitPrefix))
	decision := strings.TrimSpace(string(parts[3]))
	if decision == "" {
		return fmt.Errorf("decision payload is empty")
	}

	milestones, err := parseStagedDecisionMilestones(string(parts[1]))
	if err != nil {
		return err
	}
	switch executionUnit {
	case stagedDecisionExecutionUnitSingle:
		if len(milestones) != 0 {
			return fmt.Errorf("execution-unit single cannot include milestone definitions")
		}
	case stagedDecisionExecutionUnitMilestones:
		if len(milestones) > 0 {
			return validateStagedDecisionMilestones(milestones)
		}
	default:
		return fmt.Errorf("execution unit must be %q or %q", stagedDecisionExecutionUnitSingle, stagedDecisionExecutionUnitMilestones)
	}
	return nil
}

func parseStagedDecisionMilestones(line string) ([]stagedDecisionMilestoneDefinition, error) {
	payload := strings.TrimSpace(strings.TrimPrefix(line, decisionMilestonesPrefix))
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	var input stagedDecisionMilestoneInput
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("execution milestone payload is invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("execution milestone payload has trailing JSON")
	}
	return input.Milestones, nil
}

func validateStagedDecisionMilestones(definitions []stagedDecisionMilestoneDefinition) error {
	if len(definitions) < 2 || len(definitions) > stagedDecisionMilestoneMaxCount {
		return fmt.Errorf("execution milestones require 2-%d entries", stagedDecisionMilestoneMaxCount)
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Scope = strings.TrimSpace(definition.Scope)
		definition.Acceptance = strings.TrimSpace(definition.Acceptance)
		if definition.ID == "" || len(definition.ID) > stagedDecisionMilestoneMaxIDBytes {
			return fmt.Errorf("execution milestone id must be 1-%d bytes", stagedDecisionMilestoneMaxIDBytes)
		}
		if _, exists := seen[definition.ID]; exists {
			return fmt.Errorf("duplicate execution milestone id %q", definition.ID)
		}
		if definition.Scope == "" || len(definition.Scope) > stagedDecisionMilestoneMaxTextBytes {
			return fmt.Errorf("execution milestone %q scope must be 1-%d bytes", definition.ID, stagedDecisionMilestoneMaxTextBytes)
		}
		if definition.Acceptance == "" || len(definition.Acceptance) > stagedDecisionMilestoneMaxTextBytes {
			return fmt.Errorf("execution milestone %q acceptance must be 1-%d bytes", definition.ID, stagedDecisionMilestoneMaxTextBytes)
		}
		seen[definition.ID] = struct{}{}
	}
	return nil
}
