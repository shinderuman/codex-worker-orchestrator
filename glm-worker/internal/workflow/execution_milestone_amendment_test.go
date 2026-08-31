package workflow

import (
	"strings"
	"testing"
)

func TestExecutionMilestonePromptDistinguishesExplicitAmendmentFromRedo(t *testing.T) {
	plan := &executionMilestonePlan{
		Version:            executionMilestonePlanVersion,
		TaskID:             "task",
		ActiveTaskPath:     "IMPLEMENTATION_TASKS/large.md",
		TaskContractSHA256: "new-task-hash",
		CurrentIndex:       1,
		Milestones: []executionMilestoneRecord{
			{
				ExecutionMilestoneDefinition: ExecutionMilestoneDefinition{
					ID: "completed", Scope: "original bounded scope", Acceptance: "original acceptance",
				},
				Status: executionMilestoneComplete,
				Completion: &executionMilestoneCompletion{
					Summary: "completed before amendment", TaskContractSHA256: "old-task-hash",
				},
			},
			{
				ExecutionMilestoneDefinition: ExecutionMilestoneDefinition{
					ID: "amendment", Scope: "apply explicit semantic delta to completed behavior", Acceptance: "amended behavior passes",
				},
				Status: executionMilestonePending,
			},
		},
	}
	block, err := executionMilestonePromptBlock(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"task_contract_sha256":"new-task-hash"`,
		`"task_contract_sha256":"old-task-hash"`,
		"Do not redo completed milestones merely because a new worker begins",
		"current.scope explicitly requires the semantic delta",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("milestone prompt missing %q: %s", want, block)
		}
	}
}
