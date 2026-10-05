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
	successor, err := terminalRetirementSuccessor(schedule, updatedTasks, blocker)
	if err != nil {
		return TerminalMetadata{}, err
	}
	updatedPlan, err := taskcontract.RetirePlanTask(string(plan), target, successor)
	if err != nil {
		return TerminalMetadata{}, err
	}
	if err := validateRetirementCorpus(taskcontract.ParsePlanSchedule(updatedPlan), updatedTasks); err != nil {
		return TerminalMetadata{}, err
	}
	changes, err := terminalMetadataChanges([]byte(updatedPlan), updatedTasks, changedPaths, target)
	if err != nil {
		return TerminalMetadata{}, err
	}
	return TerminalMetadata{Changes: changes, Successor: successor}, nil
}

func terminalRetirementSuccessor(schedule taskcontract.PlanSchedule, tasks map[string][]byte, blocker bool) (string, error) {
	if blocker {
		return "", nil
	}
	if len(schedule.Next) == 0 {
		return "", fmt.Errorf("terminal focus has no mechanical NEXT successor; parent scheduling decision required")
	}
	successor := schedule.Next[0]
	deps, err := taskcontract.ParseTaskDependencyState(tasks[successor])
	if err != nil || len(deps.Outstanding) != 0 {
		return "", fmt.Errorf("first NEXT requires dependency/priority decision")
	}
	return successor, nil
}

func terminalMetadataChanges(plan []byte, tasks map[string][]byte, changedPaths []string, target string) ([]TerminalMetadataChange, error) {
	sort.Strings(changedPaths)
	changes := make([]TerminalMetadataChange, 0, len(changedPaths))
	for _, path := range changedPaths {
		change := TerminalMetadataChange{Path: path}
		switch path {
		case target:
			change.Delete = true
		case "IMPLEMENTATION_PLAN.local.md":
			change.NewBytes = plan
		default:
			content, ok := tasks[path]
			if !ok {
				return nil, fmt.Errorf("terminal metadata changed Task is missing: %s", path)
			}
			change.NewBytes = content
		}
		changes = append(changes, change)
	}
	return changes, nil
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
