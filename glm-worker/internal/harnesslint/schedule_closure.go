package harnesslint

import (
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
	failures := taskcontract.ParsePlanSchedule(string(plan)).ClosureFailures(entries)
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
	return violations, nil
}

func scheduleClosureViolationPath(failure taskcontract.ScheduleClosureFailure) string {
	if failure.Path == "" {
		return implementationPlanPath
	}
	return failure.Path
}
