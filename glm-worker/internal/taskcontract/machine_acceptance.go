package taskcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type MachineAcceptance struct {
	Present      bool
	Requirements []MachineAcceptanceRequirement
}

type MachineAcceptanceFact string

type MachineAcceptanceRequirement struct {
	ID   string                `json:"id"`
	Fact MachineAcceptanceFact `json:"fact"`
}

type machineAcceptanceDocument struct {
	Schema       string                         `json:"schema"`
	Requirements []MachineAcceptanceRequirement `json:"requirements"`
}

const (
	MachineAcceptanceHeading = "## Machine-verifiable acceptance"
	MachineAcceptanceSchema  = "task-machine-acceptance/v1"

	MachineFactFailurePathAdvisoryObserved MachineAcceptanceFact = "failure-path-advisory-observed"
	MachineFactFailurePathAdvisoryShown    MachineAcceptanceFact = "failure-path-advisory-shown"

	machineAcceptanceMaxRequirements = 16
)

var machineAcceptanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var machineAcceptanceFacts = map[MachineAcceptanceFact]bool{
	MachineFactFailurePathAdvisoryObserved: true,
	MachineFactFailurePathAdvisoryShown:    true,
}

func ParseMachineAcceptance(content []byte) (MachineAcceptance, error) {
	lines := strings.Split(string(content), "\n")
	headingAt, err := findUniqueTaskSection(lines, MachineAcceptanceHeading)
	if err != nil {
		return MachineAcceptance{}, err
	}
	if headingAt < 0 {
		return MachineAcceptance{}, nil
	}
	body := machineAcceptanceBody(lines, headingAt)
	if body == "" {
		return MachineAcceptance{}, fmt.Errorf("%s節のJSONが空です", MachineAcceptanceHeading)
	}
	document, err := decodeMachineAcceptanceDocument(body)
	if err != nil {
		return MachineAcceptance{}, err
	}
	if err := validateMachineAcceptanceDocument(document); err != nil {
		return MachineAcceptance{}, err
	}
	return MachineAcceptance{Present: true, Requirements: document.Requirements}, nil
}

func machineAcceptanceBody(lines []string, headingAt int) string {
	var body []string
	for index := headingAt + 1; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			break
		}
		body = append(body, lines[index])
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func decodeMachineAcceptanceDocument(body string) (machineAcceptanceDocument, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	decoder.DisallowUnknownFields()
	var document machineAcceptanceDocument
	if err := decoder.Decode(&document); err != nil {
		return machineAcceptanceDocument{}, fmt.Errorf("%s節のJSONを解析できません: %w", MachineAcceptanceHeading, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return machineAcceptanceDocument{}, fmt.Errorf("%s節にはJSON objectを1つだけ置いてください", MachineAcceptanceHeading)
		}
		return machineAcceptanceDocument{}, fmt.Errorf("%s節のJSON末尾が不正です: %w", MachineAcceptanceHeading, err)
	}
	return document, nil
}

func validateMachineAcceptanceDocument(document machineAcceptanceDocument) error {
	if document.Schema != MachineAcceptanceSchema {
		return fmt.Errorf("%s節のschemaが不正です: %q", MachineAcceptanceHeading, document.Schema)
	}
	if len(document.Requirements) == 0 {
		return fmt.Errorf("%s節のrequirementsが空です", MachineAcceptanceHeading)
	}
	if len(document.Requirements) > machineAcceptanceMaxRequirements {
		return fmt.Errorf("%s節のrequirementsが上限%d件を超えています", MachineAcceptanceHeading, machineAcceptanceMaxRequirements)
	}
	seen := make(map[string]struct{}, len(document.Requirements))
	for index, requirement := range document.Requirements {
		if !machineAcceptanceIDPattern.MatchString(requirement.ID) {
			return fmt.Errorf("%s節requirements[%d]のidが不正です: %q", MachineAcceptanceHeading, index, requirement.ID)
		}
		if _, duplicate := seen[requirement.ID]; duplicate {
			return fmt.Errorf("%s節のrequirement id %qが重複しています", MachineAcceptanceHeading, requirement.ID)
		}
		seen[requirement.ID] = struct{}{}
		if !machineAcceptanceFacts[requirement.Fact] {
			return fmt.Errorf("%s節requirements[%d]のfactが未対応です: %q", MachineAcceptanceHeading, index, requirement.Fact)
		}
	}
	return nil
}
