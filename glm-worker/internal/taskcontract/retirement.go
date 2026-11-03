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
	result, err := retirePlanScheduleLines(plan, target, successor)
	if err != nil {
		return "", err
	}
	updated := strings.Join(result, "\n")
	if err := validateTerminalScheduleRetirementResult(updated, target, successor); err != nil {
		return "", err
	}
	return updated, nil
}

func retirePlanScheduleLines(plan, target, successor string) ([]string, error) {
	lines := strings.Split(plan, "\n")
	result := make([]string, 0, len(lines))
	section := planScheduleSection("")
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			section = retirementPlanSection(line)
		}
		updated, removed, err := retirePlanScheduleLine(line, section, target, successor)
		if err != nil {
			return nil, err
		}
		if removed || (section != "" && updated == "" && len(result) != 0 && result[len(result)-1] == "") {
			continue
		}
		result = append(result, updated)
	}
	return result, nil
}

func retirePlanScheduleLine(line string, section planScheduleSection, target, successor string) (string, bool, error) {
	if section == "" || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "## ") {
		return line, false, nil
	}
	updated, err := retirementPlanLine(line, section, target, successor)
	return updated, updated == "", err
}

func validateTerminalScheduleRetirementResult(plan, target, successor string) error {
	schedule := ParsePlanSchedule(plan)
	active, err := schedule.ValidateComplete()
	if err != nil {
		return err
	}
	for _, path := range append(append([]string{active}, schedule.Next...), schedule.Blocked...) {
		if path == target || (successor != "" && path == successor && active != successor) {
			return fmt.Errorf("terminal schedule retirement retained a retired or duplicated entry")
		}
	}
	if successor != "" && active != successor {
		return fmt.Errorf("terminal schedule retirement did not promote the successor")
	}
	return nil
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
			if strings.HasPrefix(strings.TrimSpace(line), "- [`") {
				return "- [`" + successor + "`](" + successor + ")", nil
			}
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
	if count != 1 || (active == target && (len(schedule.Next) == 0 || schedule.Next[0] != successor)) || (active != target && successor != "") {
		return fmt.Errorf("terminal schedule retirement is not mechanically determined")
	}
	return nil
}
