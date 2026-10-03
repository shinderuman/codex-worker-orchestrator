package taskcontract

import (
	"fmt"
	"strings"
)

func RetireTaskDependency(content []byte, path string) ([]byte, bool, error) {
	state, err := ParseTaskDependencyState(content)
	if err != nil {
		return nil, false, err
	}
	if !containsSchedulePath(state.Outstanding, path) {
		return append([]byte(nil), content...), false, nil
	}
	lines := strings.Split(string(content), "\n")
	start, err := findUniqueTaskSection(lines, TaskDependenciesHeading)
	if err != nil {
		return nil, false, err
	}
	end := retirementSectionEnd(lines, start)
	body, err := retirementDependencyBody(lines[start+1:end], path)
	if err != nil {
		return nil, false, err
	}
	lines = append(append(append([]string{}, lines[:start+1]...), body...), lines[end:]...)
	fulfilled, err := findUniqueTaskSection(lines, TaskFulfilledDependenciesHeading)
	if err != nil {
		return nil, false, err
	}
	entry := "- `" + path + "`"
	if fulfilled < 0 {
		lines = append(lines, "", TaskFulfilledDependenciesHeading, "", entry, "")
	} else {
		end = retirementSectionEnd(lines, fulfilled)
		body, err = retirementDependencyBody(lines[fulfilled+1:end], "")
		if err != nil {
			return nil, false, err
		}
		if len(state.Fulfilled) == 0 {
			body = []string{"", entry, ""}
		} else {
			body = append(body, entry, "")
		}
		lines = append(append(append([]string{}, lines[:fulfilled+1]...), body...), lines[end:]...)
	}
	result := []byte(strings.Join(lines, "\n"))
	if _, err := ParseTaskDependencyState(result); err != nil {
		return nil, false, err
	}
	return result, true, nil
}

func retirementSectionEnd(lines []string, start int) int {
	for index := start + 1; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			return index
		}
	}
	return len(lines)
}

func retirementDependencyBody(lines []string, remove string) ([]string, error) {
	var body []string
	seen := map[string]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "none" {
			continue
		}
		item, ok := dependencyListItem(line)
		if !ok {
			return nil, fmt.Errorf("dependency retirement requires parent decision for %q", line)
		}
		path, referenced, err := dependencyItemPath(item)
		if err != nil || !referenced || seen[path] {
			return nil, fmt.Errorf("ambiguous dependency retirement item %q", line)
		}
		seen[path] = true
		if path != remove {
			body = append(body, line)
		}
	}
	if len(body) == 0 {
		body = []string{"none"}
	}
	return append(append([]string{""}, body...), ""), nil
}

func RetirePlanTask(plan, target, successor string) (string, error) {
	if err := validateTerminalScheduleRetirement(plan, target, successor); err != nil {
		return "", err
	}
	lines := strings.Split(plan, "\n")
	section := planScheduleSection("")
	for index, line := range lines {
		if strings.HasPrefix(line, "## ") {
			section = retirementPlanSection(line)
			continue
		}
		if section == "" || strings.TrimSpace(line) == "" {
			continue
		}
		updated, err := retirementPlanLine(line, section, target, successor)
		if err != nil {
			return "", err
		}
		lines[index] = updated
	}
	return strings.Join(lines, "\n"), nil
}

func retirementPlanSection(line string) planScheduleSection {
	heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
	for _, section := range []planScheduleSection{planScheduleActive, planScheduleNext, planScheduleBlocked} {
		if scheduleHeadingMatches(section, heading) {
			return section
		}
	}
	return ""
}

func retirementPlanLine(line string, section planScheduleSection, target, successor string) (string, error) {
	path, err := scheduleEntryPath(section, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- ")))
	if err != nil {
		return "", err
	}
	if path == target {
		if section == planScheduleActive {
			return "- `" + successor + "`", nil
		}
		return "", nil
	} else if successor != "" && path == successor && section == planScheduleNext {
		return "", nil
	}
	return line, nil
}

func validateTerminalScheduleRetirement(plan, target, successor string) error {
	schedule := ParsePlanSchedule(plan)
	active, err := schedule.ValidateComplete()
	if err != nil {
		return err
	}
	count := 0
	for _, path := range append(append([]string{active}, schedule.Next...), schedule.Blocked...) {
		if path == target {
			count++
		}
	}
	if count != 1 || (active == target && successor == "") || (active != target && successor != "") {
		return fmt.Errorf("terminal schedule retirement is not mechanically determined")
	}
	return nil
}
