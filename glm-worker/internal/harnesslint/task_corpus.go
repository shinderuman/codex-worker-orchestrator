package harnesslint

import (
	"fmt"
	"strings"
)

type taskCorpusLine struct {
	number int
	text   string
}

const taskCorpusPrefix = "IMPLEMENTATION_TASKS/"

func taskCorpusViolations(root string, paths []string) ([]Violation, error) {
	existing := make(map[string]bool)
	for _, path := range paths {
		if isTaskCorpusPath(path) {
			existing[path] = true
		}
	}

	var violations []Violation
	for _, path := range paths {
		if !existing[path] {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, taskFileCorpusViolations(path, string(data), existing)...)
	}
	return violations, nil
}

func isTaskCorpusPath(path string) bool {
	return strings.HasPrefix(path, taskCorpusPrefix) && strings.HasSuffix(path, ".md")
}

func taskFileCorpusViolations(taskPath, body string, existing map[string]bool) []Violation {
	var violations []Violation
	inDependencies := false
	for _, line := range taskCorpusSectionLines(body) {
		if strings.HasPrefix(line.text, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line.text, "## "))
			inDependencies = heading == "Dependencies"
			if heading == "Status" {
				violations = append(violations, Violation{
					Rule: "task-corpus-integrity", Path: taskPath, Line: line.number, Column: 1,
					Message: "task file must not contain a top-level Status section; schedule state belongs to the Plan",
				})
			}
			continue
		}
		if !inDependencies {
			continue
		}
		trimmed := strings.TrimSpace(line.text)
		if !strings.HasPrefix(trimmed, "- `IMPLEMENTATION_TASKS/") || !strings.HasSuffix(trimmed, "`") {
			continue
		}
		ref := strings.TrimSuffix(strings.TrimPrefix(trimmed, "- `"), "`")
		if existing[ref] {
			continue
		}
		violations = append(violations, Violation{
			Rule: "task-corpus-integrity", Path: taskPath, Line: line.number, Column: 1,
			Message: fmt.Sprintf("dependency %s does not exist; remove fulfilled dependencies and retain required history as a Historical invariant", ref),
		})
	}
	return violations
}

func taskCorpusSectionLines(body string) []taskCorpusLine {
	var lines []taskCorpusLine
	fence := 0
	for index, line := range strings.Split(body, "\n") {
		backticks := taskCorpusLeadingBackticks(line)
		if fence > 0 {
			if backticks >= fence {
				fence = 0
			}
			continue
		}
		if backticks >= 3 {
			fence = backticks
			continue
		}
		lines = append(lines, taskCorpusLine{number: index + 1, text: line})
	}
	return lines
}

func taskCorpusLeadingBackticks(line string) int {
	trimmed := strings.TrimLeft(line, " \t")
	count := 0
	for count < len(trimmed) && trimmed[count] == '`' {
		count++
	}
	return count
}
