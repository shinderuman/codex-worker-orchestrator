package harnesslint

import (
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const implementationPlanPath = "IMPLEMENTATION_PLAN.local.md"

func activeTaskContractViolations(root string, paths []string) ([]Violation, error) {
	if !containsPath(paths, implementationPlanPath) {
		return nil, nil
	}
	plan, err := readRegularFile(root, implementationPlanPath)
	if err != nil {
		return nil, err
	}
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	taskPath, err := schedule.ActiveTask()
	if err != nil {
		return []Violation{activeTaskContractViolation(implementationPlanPath, err.Error())}, nil
	}
	if !containsPath(paths, taskPath) {
		return []Violation{activeTaskContractViolation(taskPath, "Plan-selected ACTIVE task file is missing")}, nil
	}
	task, err := readRegularFile(root, taskPath)
	if err != nil {
		return nil, err
	}
	if _, err := taskcontract.ParseExternalFeasibility(task); err != nil {
		return []Violation{activeTaskContractViolation(taskPath, err.Error())}, nil
	}
	return nil, nil
}

func activeTaskContractViolation(path, message string) Violation {
	return Violation{
		Rule:    "active-task-contract",
		Path:    path,
		Line:    1,
		Column:  1,
		Message: message,
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if strings.TrimSpace(path) == target {
			return true
		}
	}
	return false
}
