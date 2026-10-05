package repositoryproject

import (
	"fmt"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type TerminalMetadataChange struct {
	Path     string
	NewBytes []byte
	Delete   bool
}

type TerminalMetadata struct {
	Changes   []TerminalMetadataChange
	Successor string
}

func RetireTerminalMetadata(plan []byte, tasks map[string][]byte, target string, blocker bool) (TerminalMetadata, error) {
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	active, err := schedule.ValidateComplete()
	if err != nil {
		return TerminalMetadata{}, err
	}
	if _, ok := tasks[target]; !ok || blocker == (active == target) {
		return TerminalMetadata{}, fmt.Errorf("terminal Task/focus identity is invalid")
	}
	if err := validateRetirementCorpus(schedule, tasks); err != nil {
		return TerminalMetadata{}, err
	}
	updatedTasks, changedPaths, err := retireInboundDependencies(tasks, target, []string{target, "IMPLEMENTATION_PLAN.local.md"})
	if err != nil {
		return TerminalMetadata{}, err
	}
	result := TerminalMetadata{}
	if !blocker {
		if len(schedule.Next) == 0 {
			return TerminalMetadata{}, fmt.Errorf("terminal focus has no mechanical NEXT successor; parent scheduling decision required")
		}
		result.Successor = schedule.Next[0]
		deps, err := taskcontract.ParseTaskDependencyState(updatedTasks[result.Successor])
		if err != nil || len(deps.Outstanding) != 0 {
			return TerminalMetadata{}, fmt.Errorf("first NEXT requires dependency/priority decision")
		}
	}
	updatedPlan, err := taskcontract.RetirePlanTask(string(plan), target, result.Successor)
	if err != nil {
		return TerminalMetadata{}, err
	}
	if err := validateRetirementCorpus(taskcontract.ParsePlanSchedule(updatedPlan), updatedTasks); err != nil {
		return TerminalMetadata{}, err
	}
	sort.Strings(changedPaths)
	result.Changes = make([]TerminalMetadataChange, 0, len(changedPaths))
	for _, path := range changedPaths {
		change := TerminalMetadataChange{Path: path}
		switch path {
		case target:
			change.Delete = true
		case "IMPLEMENTATION_PLAN.local.md":
			change.NewBytes = []byte(updatedPlan)
		default:
			content, ok := updatedTasks[path]
			if !ok {
				return TerminalMetadata{}, fmt.Errorf("terminal metadata changed Task is missing: %s", path)
			}
			change.NewBytes = content
		}
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

func validateRetirementCorpus(schedule taskcontract.PlanSchedule, tasks map[string][]byte) error {
	var entries []taskcontract.TaskCorpusEntry
	for path := range tasks {
		entries = append(entries, taskcontract.TaskCorpusEntry{Path: path, Regular: true})
	}
	if _, err := schedule.ValidateComplete(); err != nil {
		return err
	}
	if err := ValidateClosure(schedule, entries, "terminal metadata corpus is invalid"); err != nil {
		return err
	}
	paths := append(append(append([]string{}, schedule.Active...), schedule.Next...), schedule.Blocked...)
	_, err := BuildTaskGraph(paths, tasks)
	return err
}

func retireInboundDependencies(tasks map[string][]byte, target string, changedPaths []string) (map[string][]byte, []string, error) {
	updatedTasks := map[string][]byte{}
	for path, content := range tasks {
		if path == target {
			continue
		}
		updated, changed, err := taskcontract.RetireTaskDependency(content, target)
		if err != nil {
			return nil, nil, fmt.Errorf("retire inbound dependency %s: %w", path, err)
		}
		updatedTasks[path] = updated
		if changed {
			changedPaths = append(changedPaths, path)
		}
	}
	return updatedTasks, changedPaths, nil
}
