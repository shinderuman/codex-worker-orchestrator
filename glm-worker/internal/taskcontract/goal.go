package taskcontract

import (
	"fmt"
	"strings"
)

type PlanGoal struct {
	Present bool
	Status  string
}

const (
	PlanGoalHeading = "## GOAL"

	GoalStatusActive    = "active"
	GoalStatusCompleted = "completed"

	goalStatusKey = "status"
)

var goalStatusValues = []string{GoalStatusActive, GoalStatusCompleted}

func ParsePlanGoal(planContent string) (PlanGoal, error) {
	lines := strings.Split(planContent, "\n")
	headingAt, err := findGoalSection(lines)
	if err != nil {
		return PlanGoal{}, err
	}
	if headingAt < 0 {
		return PlanGoal{}, nil
	}
	status, err := parseGoalStatus(lines, headingAt)
	if err != nil {
		return PlanGoal{}, err
	}
	return PlanGoal{Present: true, Status: status}, nil
}

func findGoalSection(lines []string) (int, error) {
	headingAt := -1
	sections := 0
	fence := 0
	for index, line := range lines {
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") && strings.TrimSpace(line) == PlanGoalHeading {
			sections++
			if headingAt < 0 {
				headingAt = index
			}
		}
	}
	if sections > 1 {
		return -1, fmt.Errorf("GOAL節が複数あります(%d節)", sections)
	}
	return headingAt, nil
}

func parseGoalStatus(lines []string, headingAt int) (string, error) {
	status := ""
	seen := false
	fence := 0
	for index := headingAt + 1; index < len(lines); index++ {
		line := lines[index]
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}
		key, value, _ := strings.Cut(trimmed, ":")
		if strings.TrimSpace(key) != goalStatusKey {
			continue
		}
		if seen {
			return "", fmt.Errorf("GOAL節の%sが複数あります", goalStatusKey)
		}
		value = strings.TrimSpace(value)
		if !knownGoalStatus(value) {
			return "", fmt.Errorf("GOAL節の%s %qは未知です(%s)", goalStatusKey, value, strings.Join(goalStatusValues, "/"))
		}
		status = value
		seen = true
	}
	if !seen {
		return "", fmt.Errorf("GOAL節に%s: %s形式の行がありません(%s)", goalStatusKey, strings.Join(goalStatusValues, "/"), goalStatusKey)
	}
	return status, nil
}

func knownGoalStatus(value string) bool {
	for _, known := range goalStatusValues {
		if value == known {
			return true
		}
	}
	return false
}
