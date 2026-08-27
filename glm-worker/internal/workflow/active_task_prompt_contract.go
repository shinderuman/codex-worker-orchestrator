package workflow

import (
	"fmt"
	"strings"
)

type activeTaskPromptAudience string

type activeTaskRequirement uint16

type activeTaskPromptContract struct {
	path                  string
	audience              activeTaskPromptAudience
	requirements          activeTaskRequirement
	parentManaged         bool
	verifyDerivedContract bool
}

const (
	activeTaskAudienceWorker   activeTaskPromptAudience = "worker"
	activeTaskAudienceReviewer activeTaskPromptAudience = "reviewer"
)

const (
	activeTaskOriginalInstruction activeTaskRequirement = 1 << iota
	activeTaskAmendments
	activeTaskResolvedReferences
	activeTaskContract
	activeTaskMustNot
	activeTaskAcceptanceCriteria
	activeTaskHistoricalInvariants
)

const activeTaskWorkerRequirements = activeTaskOriginalInstruction |
	activeTaskAmendments |
	activeTaskResolvedReferences |
	activeTaskContract |
	activeTaskMustNot |
	activeTaskAcceptanceCriteria

const activeTaskReviewerRequirements = activeTaskWorkerRequirements | activeTaskHistoricalInvariants

func newActiveTaskPromptContract(path string, audience activeTaskPromptAudience) activeTaskPromptContract {
	contract := activeTaskPromptContract{
		path:          path,
		audience:      audience,
		parentManaged: true,
	}
	switch audience {
	case activeTaskAudienceReviewer:
		contract.requirements = activeTaskReviewerRequirements
		contract.verifyDerivedContract = true
	default:
		contract.requirements = activeTaskWorkerRequirements
	}
	return contract
}

func (c activeTaskPromptContract) valid() bool {
	if c.path == "" || !c.parentManaged {
		return false
	}
	switch c.audience {
	case activeTaskAudienceWorker:
		return c.requirements == activeTaskWorkerRequirements && !c.verifyDerivedContract
	case activeTaskAudienceReviewer:
		return c.requirements == activeTaskReviewerRequirements && c.verifyDerivedContract
	default:
		return false
	}
}

func renderActiveTaskPromptContract(contract activeTaskPromptContract) string {
	if contract.path == "" {
		return ""
	}
	if !contract.valid() {
		panic("invalid active task prompt contract")
	}

	var block strings.Builder
	block.WriteString("ACTIVE_TASK_CONTEXT:\n")
	block.WriteString("PATH: ")
	block.WriteString(contract.path)
	block.WriteString("\nAUDIENCE: ")
	block.WriteString(string(contract.audience))
	block.WriteString("\nSOURCE_AUTHORITY: active-task-file\n")
	block.WriteString("REQUIRED_SECTIONS: ")
	block.WriteString(strings.Join(activeTaskRequirementNames(contract.requirements), ","))
	block.WriteString("\nPARENT_MANAGED: true\n")
	block.WriteString("DERIVED_CONTRACT_REVIEW: ")
	block.WriteString(fmt.Sprintf("%t", contract.verifyDerivedContract))
	block.WriteString("\nEND_ACTIVE_TASK_CONTEXT\n")
	return block.String()
}

func activeTaskRequirementNames(requirements activeTaskRequirement) []string {
	ordered := []struct {
		flag activeTaskRequirement
		name string
	}{
		{activeTaskOriginalInstruction, "original-instruction"},
		{activeTaskAmendments, "amendments"},
		{activeTaskResolvedReferences, "resolved-references"},
		{activeTaskContract, "contract"},
		{activeTaskMustNot, "must-not"},
		{activeTaskAcceptanceCriteria, "acceptance-criteria"},
		{activeTaskHistoricalInvariants, "historical-invariants"},
	}
	names := make([]string, 0, len(ordered))
	for _, item := range ordered {
		if requirements&item.flag != 0 {
			names = append(names, item.name)
		}
	}
	return names
}
