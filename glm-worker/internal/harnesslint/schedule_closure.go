package harnesslint

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const scheduleClosureRule = "task-schedule-closure"

func taskScheduleClosureViolations(root string, paths []string) ([]Violation, error) {
	if !containsPath(paths, implementationPlanPath) {
		return nil, nil
	}
	plan, err := readRegularFile(root, implementationPlanPath)
	if err != nil {
		return nil, err
	}
	entries, err := taskcontract.EnumerateTaskCorpus(root)
	if err != nil {
		return nil, err
	}
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	failures := schedule.ClosureFailures(entries)
	violations := make([]Violation, 0, len(failures))
	for _, failure := range failures {
		violations = append(violations, Violation{
			Rule:    scheduleClosureRule,
			Path:    scheduleClosureViolationPath(failure),
			Line:    1,
			Column:  1,
			Message: failure.Reason,
		})
	}
	if len(violations) > 0 {
		return violations, nil
	}
	ordering, err := finalVerificationOrderingViolations(root, schedule)
	if err != nil {
		return nil, err
	}
	return append(violations, ordering...), nil
}

func finalVerificationOrderingViolations(root string, schedule taskcontract.PlanSchedule) ([]Violation, error) {
	candidates, err := schedule.TasksScheduledAfterFinalVerification()
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, path := range candidates {
		task, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		dependencies, err := taskcontract.ParseTaskDependencyState(task)
		if err != nil {
			return nil, fmt.Errorf("task %s: %w", path, err)
		}
		if len(dependencies.Outstanding) > 0 {
			continue
		}
		violations = append(violations, Violation{
			Rule:    scheduleClosureRule,
			Path:    implementationPlanPath,
			Line:    1,
			Column:  1,
			Message: fmt.Sprintf("final verification task %sより後ろにrunnable unblocked task %sがあります", taskcontract.FinalVerificationTaskPath, path),
		})
	}
	return violations, nil
}

func scheduleClosureViolationPath(failure taskcontract.ScheduleClosureFailure) string {
	if failure.Path == "" {
		return implementationPlanPath
	}
	return failure.Path
}
