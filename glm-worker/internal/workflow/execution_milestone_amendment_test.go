package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
)

func TestExecutionMilestonePromptDistinguishesExplicitAmendmentFromRedo(t *testing.T) {
	plan := &executionmilestone.Plan{
		Version:            executionmilestone.PlanVersion,
		TaskID:             "task",
		ActiveTaskPath:     "IMPLEMENTATION_TASKS/large.md",
		TaskContractSHA256: "new-task-hash",
		CurrentIndex:       1,
		Milestones: []executionmilestone.Record{
			{
				MilestoneDefinition: executionunit.MilestoneDefinition{
					ID: "completed", Scope: "original bounded scope", Acceptance: "original acceptance",
				},
				Status: executionmilestone.StatusComplete,
				Completion: &executionmilestone.Completion{
					Summary: "completed before amendment", TaskContractSHA256: "old-task-hash",
				},
			},
			{
				MilestoneDefinition: executionunit.MilestoneDefinition{
					ID: "amendment", Scope: "apply explicit semantic delta to completed behavior", Acceptance: "amended behavior passes",
				},
				Status: executionmilestone.StatusPending,
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
