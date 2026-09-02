package taskcontract

import (
	"fmt"
	"strings"
)

type ReviewFindings struct {
	Present bool
	None    bool
}

const (
	TaskDependenciesHeading = "## Dependencies"

	ReviewFindingsHeading = "## Review findings"

	reviewFindingsNone = "none"
)

func ParseTaskDependencies(content []byte) ([]string, error) {
	lines := strings.Split(string(content), "\n")
	headingAt, err := findUniqueTaskSection(lines, TaskDependenciesHeading)
	if err != nil {
		return nil, err
	}
	if headingAt < 0 {
		return nil, fmt.Errorf("%s節がありません", TaskDependenciesHeading)
	}
	return parseDependencyPaths(lines, headingAt)
}

func parseDependencyPaths(lines []string, headingAt int) ([]string, error) {
	var paths []string
	seen := map[string]bool{}
	fence := 0
	for index := headingAt + 1; index < len(lines); index++ {
		line := lines[index]
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		item, ok := dependencyListItem(line)
		if !ok {
			continue
		}
		path, referenced, err := dependencyItemPath(item)
		if err != nil {
			return nil, err
		}
		if !referenced {
			continue
		}
		if err := ValidateActiveTaskPath(path); err != nil {
			return nil, err
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths, nil
}

func dependencyListItem(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), true
}

func dependencyItemPath(item string) (string, bool, error) {
	if !strings.Contains(item, TasksDir+"/") {
		return "", false, nil
	}
	switch strings.Count(item, "`") {
	case 0:
		return item, true, nil
	case 2:
		if strings.HasPrefix(item, "`") && strings.HasSuffix(item, "`") {
			return item[1 : len(item)-1], true, nil
		}
	}
	return "", false, fmt.Errorf("dependency項目 %qがDependencies欄のtask path参照記法(逆引用符1組で囲まれた単一task path、または逆引用符なしの直書き)へ違反しています", item)
}

func ParseReviewFindings(content []byte) (ReviewFindings, error) {
	lines := strings.Split(string(content), "\n")
	headingAt, err := findUniqueTaskSection(lines, ReviewFindingsHeading)
	if err != nil {
		return ReviewFindings{}, err
	}
	if headingAt < 0 {
		return ReviewFindings{}, fmt.Errorf("%s節がありません", ReviewFindingsHeading)
	}
	return ReviewFindings{Present: true, None: reviewFindingsBody(lines, headingAt) == reviewFindingsNone}, nil
}

func reviewFindingsBody(lines []string, headingAt int) string {
	var body []string
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
		if trimmed == "" {
			continue
		}
		body = append(body, trimmed)
	}
	return strings.Join(body, "\n")
}

func findUniqueTaskSection(lines []string, heading string) (int, error) {
	headingAt := -1
	sections := 0
	fence := 0
	for index, line := range lines {
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") && strings.TrimSpace(line) == heading {
			sections++
			if headingAt < 0 {
				headingAt = index
			}
		}
	}
	if sections > 1 {
		return -1, fmt.Errorf("%s節が複数あります(%d節)", heading, sections)
	}
	return headingAt, nil
}
