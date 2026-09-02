package taskcontract

import (
	"fmt"
	"strings"
)

type PlanSchedule struct {
	Active  []string
	Next    []string
	Blocked []string

	activeErr  error
	nextErr    error
	blockedErr error
}

type planScheduleSection string

const (
	planScheduleActive  planScheduleSection = "ACTIVE"
	planScheduleNext    planScheduleSection = "NEXT"
	planScheduleBlocked planScheduleSection = "BLOCKED"
)

func ParsePlanSchedule(planContent string) PlanSchedule {
	active, activeErr := parsePlanScheduleSection(planContent, planScheduleActive)
	next, nextErr := parsePlanScheduleSection(planContent, planScheduleNext)
	blocked, blockedErr := parsePlanScheduleSection(planContent, planScheduleBlocked)
	return PlanSchedule{
		Active:     active,
		Next:       next,
		Blocked:    blocked,
		activeErr:  activeErr,
		nextErr:    nextErr,
		blockedErr: blockedErr,
	}
}

func (s PlanSchedule) ActiveEntries() ([]string, error) {
	if s.activeErr != nil {
		return nil, s.activeErr
	}
	return append([]string(nil), s.Active...), nil
}

func (s PlanSchedule) NonActiveEntries() ([]string, []string, error) {
	if s.nextErr != nil {
		return nil, nil, s.nextErr
	}
	if s.blockedErr != nil {
		return nil, nil, s.blockedErr
	}
	return append([]string(nil), s.Next...), append([]string(nil), s.Blocked...), nil
}

func (s PlanSchedule) ActiveTask() (string, error) {
	if s.activeErr != nil {
		return "", s.activeErr
	}
	switch len(s.Active) {
	case 0:
		return "", fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄にtask fileがありません")
	case 1:
		if err := ValidateActiveTaskPath(s.Active[0]); err != nil {
			return "", err
		}
		return s.Active[0], nil
	default:
		return "", fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄が一意ではありません(%d件)", len(s.Active))
	}
}

func (s PlanSchedule) ValidateComplete() (string, error) {
	active, err := s.ActiveTask()
	if err != nil {
		return "", err
	}
	if s.nextErr != nil {
		return "", s.nextErr
	}
	if s.blockedErr != nil {
		return "", s.blockedErr
	}
	for _, entries := range [][]string{s.Next, s.Blocked} {
		for _, path := range entries {
			if err := ValidateActiveTaskPath(path); err != nil {
				return "", err
			}
		}
	}
	if containsSchedulePath(s.Next, active) || containsSchedulePath(s.Blocked, active) {
		return "", fmt.Errorf("ACTIVE task file %s がNEXT/BLOCKEDへ重複して記載されています", active)
	}
	return active, nil
}

func parsePlanScheduleSection(planContent string, section planScheduleSection) ([]string, error) {
	lines := strings.Split(planContent, "\n")
	inSection := false
	var entries []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			inSection = scheduleHeadingMatches(section, heading)
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdの%s欄の行 %qがschedule list記法(`- `bulletとblank行のみ)へ違反しています", section, trimmed)
		}
		path, err := scheduleEntryPath(section, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		if err != nil {
			return nil, err
		}
		entries = append(entries, path)
	}
	return entries, nil
}

func scheduleHeadingMatches(section planScheduleSection, heading string) bool {
	if section == planScheduleActive {
		return heading == string(section)
	}
	return strings.HasPrefix(heading, string(section))
}

func scheduleEntryPath(section planScheduleSection, item string) (string, error) {
	switch strings.Count(item, "`") {
	case 0:
		return item, nil
	case 2:
		if strings.HasPrefix(item, "`") && strings.HasSuffix(item, "`") {
			return item[1 : len(item)-1], nil
		}
	}
	return "", fmt.Errorf("%s欄の項目 %qがbullet構文(逆引用符1組で囲まれた単一task path、または逆引用符なしの直書き)へ違反しています", section, item)
}

func containsSchedulePath(entries []string, target string) bool {
	for _, entry := range entries {
		if entry == target {
			return true
		}
	}
	return false
}
