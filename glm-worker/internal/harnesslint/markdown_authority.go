package harnesslint

import (
	"bytes"
	"regexp"
	"strings"
)

const markdownDerivedStateRule = "markdown-derived-state"

var readmePinnedVersionPattern = regexp.MustCompile(`(?m)^- (?:Go|golangci-lint|shellcheck|shfmt)\s+[^\n]*\d+\.\d+`)

type markdownFence struct {
	marker byte
	width  int
}

func markdownDerivedStateViolations(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range paths {
		if !strings.HasSuffix(path, ".md") {
			continue
		}
		current, err := markdownDerivedStatePathViolations(root, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, current...)
	}
	return violations, nil
}

func markdownDerivedStatePathViolations(root, path string) ([]Violation, error) {
	data, err := readRegularFile(root, path)
	if err != nil {
		return nil, err
	}
	headings := markdownLevelTwoHeadings(data)
	var violations []Violation

	switch {
	case path == "IMPLEMENTATION_PLAN.local.md":
		for _, heading := range []string{"現在のGit境界", "現在の停止理由", "次の親Codex操作"} {
			if line, ok := headings[heading]; ok {
				violations = append(violations, markdownDerivedViolation(path, line, "Plan must not duplicate live Git or transition state in a handwritten section"))
			}
		}
	case strings.HasPrefix(path, "IMPLEMENTATION_TASKS/"):
		if line, ok := headings["Current boundary"]; ok {
			violations = append(violations, markdownDerivedViolation(path, line, "task schedule/current state belongs to the Plan or runtime state, not a handwritten Current boundary"))
		}
		if line, ok := headings["Review findings"]; ok && markdownSectionBody(data, "Review findings") == "none" {
			violations = append(violations, markdownDerivedViolation(path, line, "omit Review findings when there are no unresolved findings"))
		}
	case path == "README.md":
		for _, heading := range []string{"CLI", "Lifecycle / parent action", "Evidence bundle / State"} {
			if line, ok := headings[heading]; ok {
				violations = append(violations, markdownDerivedViolation(path, line, "README must point to live CLI/source authority instead of mirroring mutable implementation inventory"))
			}
		}
		if match := readmePinnedVersionPattern.FindIndex(data); match != nil {
			line := bytes.Count(data[:match[0]], []byte("\n")) + 1
			violations = append(violations, markdownDerivedViolation(path, line, "README must point to quality-tools.yml instead of pinning tool versions"))
		}
	}
	return violations, nil
}

func markdownDerivedViolation(path string, line int, message string) Violation {
	return Violation{Rule: markdownDerivedStateRule, Path: path, Line: line, Column: 1, Message: message}
}

func markdownLevelTwoHeadings(data []byte) map[string]int {
	result := map[string]int{}
	var fence markdownFence
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if updateMarkdownFence(line, &fence) {
			continue
		}
		if fence.width != 0 || !strings.HasPrefix(line, "## ") {
			continue
		}
		result[strings.TrimSpace(strings.TrimPrefix(line, "## "))] = index + 1
	}
	return result
}

func updateMarkdownFence(line string, fence *markdownFence) bool {
	marker, width := markdownFenceRun(line)
	if width < 3 {
		return false
	}
	if fence.width == 0 {
		fence.marker = marker
		fence.width = width
		return true
	}
	if marker != fence.marker || width < fence.width {
		return false
	}
	rest := strings.TrimSpace(line[width:])
	if rest != "" {
		return false
	}
	*fence = markdownFence{}
	return true
}

func markdownFenceRun(line string) (byte, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}
	marker := line[0]
	width := 0
	for width < len(line) && line[width] == marker {
		width++
	}
	return marker, width
}

func markdownSectionBody(data []byte, heading string) string {
	lines := strings.Split(string(data), "\n")
	inSection := false
	var fence markdownFence
	var body []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if updateMarkdownFence(line, &fence) {
			if inSection {
				body = append(body, line)
			}
			continue
		}
		if fence.width == 0 && strings.HasPrefix(line, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if inSection {
				break
			}
			inSection = name == heading
			continue
		}
		if inSection && line != "" {
			body = append(body, line)
		}
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}
