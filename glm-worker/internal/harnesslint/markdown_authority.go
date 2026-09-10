package harnesslint

import (
	"regexp"
	"strings"
)

type markdownFence struct {
	marker byte
	width  int
}

type markdownSection struct {
	line int
	body string
}

const markdownDerivedStateRule = "markdown-derived-state"

var readmePinnedVersionPattern = regexp.MustCompile(`^- (?:Go|golangci-lint|shellcheck|shfmt)\s+[^\n]*\d+\.\d+`)

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
	switch {
	case path == "IMPLEMENTATION_PLAN.local.md":
		return markdownPlanDerivedStateViolations(path, headings), nil
	case strings.HasPrefix(path, "IMPLEMENTATION_TASKS/"):
		return markdownTaskDerivedStateViolations(path, data, headings), nil
	case path == "README.md":
		return markdownReadmeDerivedStateViolations(path, data, headings), nil
	default:
		return nil, nil
	}
}

func markdownPlanDerivedStateViolations(path string, headings map[string]int) []Violation {
	var violations []Violation
	for _, heading := range []string{"現在のGit境界", "現在の停止理由", "次の親Codex操作"} {
		if line, ok := headings[heading]; ok {
			violations = append(violations, markdownDerivedViolation(path, line, "Plan must not duplicate live Git or transition state in a handwritten section"))
		}
	}
	return violations
}

func markdownTaskDerivedStateViolations(path string, data []byte, headings map[string]int) []Violation {
	var violations []Violation
	if line, ok := headings["Current boundary"]; ok {
		violations = append(violations, markdownDerivedViolation(path, line, "task schedule/current state belongs to the Plan or runtime state, not a handwritten Current boundary"))
	}
	for _, section := range markdownSections(data, "Review findings") {
		if section.body == "none" {
			violations = append(violations, markdownDerivedViolation(path, section.line, "omit Review findings when there are no unresolved findings"))
		}
	}
	return violations
}

func markdownReadmeDerivedStateViolations(path string, data []byte, headings map[string]int) []Violation {
	var violations []Violation
	for _, heading := range []string{"CLI", "Lifecycle / parent action", "Evidence bundle / State"} {
		if line, ok := headings[heading]; ok {
			violations = append(violations, markdownDerivedViolation(path, line, "README must point to live CLI/source authority instead of mirroring mutable implementation inventory"))
		}
	}
	if line := markdownPinnedVersionLine(data); line > 0 {
		violations = append(violations, markdownDerivedViolation(path, line, "README must point to quality-tools.yml instead of pinning tool versions"))
	}
	return violations
}

func markdownPinnedVersionLine(data []byte) int {
	var fence markdownFence
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if updateMarkdownFence(line, &fence) || fence.width != 0 {
			continue
		}
		if readmePinnedVersionPattern.MatchString(line) {
			return index + 1
		}
	}
	return 0
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
	if strings.TrimSpace(line[width:]) != "" {
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

func markdownSections(data []byte, heading string) []markdownSection {
	lines := strings.Split(string(data), "\n")
	var sections []markdownSection
	var fence markdownFence
	sectionLine := 0
	var body []string

	flush := func() {
		if sectionLine == 0 {
			return
		}
		sections = append(sections, markdownSection{
			line: sectionLine,
			body: strings.TrimSpace(strings.Join(body, "\n")),
		})
		sectionLine = 0
		body = nil
	}

	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if updateMarkdownFence(line, &fence) {
			if sectionLine != 0 {
				body = append(body, line)
			}
			continue
		}
		if fence.width == 0 && strings.HasPrefix(line, "## ") {
			flush()
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if name == heading {
				sectionLine = index + 1
			}
			continue
		}
		if sectionLine != 0 && line != "" {
			body = append(body, line)
		}
	}
	flush()
	return sections
}
