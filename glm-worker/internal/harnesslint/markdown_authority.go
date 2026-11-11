package harnesslint

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type markdownFence struct {
	marker byte
	width  int
}

const markdownDerivedStateRule = "markdown-derived-state"

var planExtraBlankLines = regexp.MustCompile(`\n(?:[ \t]*\n){2,}`)
var planHeadingBlankLines = regexp.MustCompile(`^(?:[ \t]*\n){2,}`)
var planSeparatedTaskItems = regexp.MustCompile("(?m)(^[ \\t]*- [ \\t]*(?:IMPLEMENTATION_TASKS/|`IMPLEMENTATION_TASKS/|\\[)[^\\n]*\\n)[ \\t]*\\n([ \\t]*- [ \\t]*(?:IMPLEMENTATION_TASKS/|`IMPLEMENTATION_TASKS/|\\[))")

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
	case path == implementationPlanPath:
		return append(markdownPlanDerivedStateViolations(path, headings), markdownPlanSpacingViolations(path, data)...), nil
	case strings.HasPrefix(path, "IMPLEMENTATION_TASKS/"):
		return markdownTaskDerivedStateViolations(path, data, headings), nil
	case path == "README.md":
		return markdownReadmeDerivedStateViolations(path, data, headings), nil
	default:
		return nil, nil
	}
}

func markdownPlanSpacingViolations(path string, data []byte) []Violation {
	normalized := compactPlanSpacing(data)
	if bytes.Equal(data, normalized) {
		return nil
	}
	offset := 0
	for offset < len(normalized) && data[offset] == normalized[offset] {
		offset++
	}
	return []Violation{{
		Rule:    "markdown-plan-spacing",
		Path:    path,
		Line:    bytes.Count(data[:offset], []byte("\n")) + 1,
		Column:  1,
		Message: "Plan contains redundant blank lines",
		Fixable: true,
	}}
}

func compactPlanSpacing(data []byte) []byte {
	for _, heading := range []string{"## ACTIVE\n", "## NEXT（優先順）\n", "## BLOCKED / USER_PERMISSION_WAIT\n"} {
		data = compactPlanSectionSpacing(data, heading)
	}
	return data
}

func compactPlanSectionSpacing(data []byte, heading string) []byte {
	start, end, found := planSectionBounds(data, heading)
	if !found {
		return data
	}
	original := data[start:end]
	result := compactPlanUnfencedSpacing(original)
	if bytes.Equal(original, result) {
		return data
	}
	compacted := make([]byte, 0, len(data)-len(original)+len(result))
	compacted = append(compacted, data[:start]...)
	compacted = append(compacted, result...)
	return append(compacted, data[end:]...)
}

// Find only real level-two headings: quoted headings inside code fences are not sections.
func planSectionBounds(data []byte, heading string) (int, int, bool) {
	start := -1
	offset := 0
	var fence markdownFence
	for _, raw := range bytes.SplitAfter(data, []byte("\n")) {
		line := strings.TrimSuffix(string(raw), "\n")
		if updateMarkdownFence(strings.TrimSpace(line), &fence) {
			offset += len(raw)
			continue
		}
		if fence.width == 0 && strings.HasPrefix(line, "## ") {
			if start >= 0 {
				return start, offset, true
			}
			if line+"\n" == heading {
				start = offset + len(raw)
			}
		}
		offset += len(raw)
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(data), true
}

// Keep fenced examples byte-for-byte intact, even when they contain task bullets.
func compactPlanUnfencedSpacing(data []byte) []byte {
	var result, unfenced []byte
	var fence markdownFence
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if updateMarkdownFence(strings.TrimSpace(string(line)), &fence) {
			result = append(result, compactPlanPlainSpacing(unfenced)...)
			unfenced = nil
			result = append(result, line...)
			continue
		}
		if fence.width != 0 {
			result = append(result, line...)
		} else {
			unfenced = append(unfenced, line...)
		}
	}
	return append(result, compactPlanPlainSpacing(unfenced)...)
}

func compactPlanPlainSpacing(data []byte) []byte {
	result := planHeadingBlankLines.ReplaceAll(data, []byte("\n"))
	result = planExtraBlankLines.ReplaceAll(result, []byte("\n\n"))
	for {
		next := planSeparatedTaskItems.ReplaceAll(result, []byte("$1$2"))
		if bytes.Equal(next, result) {
			return result
		}
		result = next
	}
}

func fixPlanSpacing(root string, paths []string) error {
	if !containsPath(paths, implementationPlanPath) {
		return nil
	}
	data, err := readRegularFile(root, implementationPlanPath)
	if err != nil {
		return err
	}
	normalized := compactPlanSpacing(data)
	if bytes.Equal(data, normalized) {
		return nil
	}
	return writeRegularFile(root, implementationPlanPath, normalized)
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
	findings, err := taskcontract.ParseReviewFindings(data)
	if err != nil {
		line := headings["Review findings"]
		violations = append(violations, markdownDerivedViolation(path, line, "Review findings must have one live, unambiguous section: "+err.Error()))
	} else if findings.Present && findings.None {
		violations = append(violations, markdownDerivedViolation(path, headings["Review findings"], "omit Review findings when there are no unresolved findings"))
	}
	if _, err := taskcontract.ParseMachineAcceptance(data); err != nil {
		line := headings[strings.TrimPrefix(taskcontract.MachineAcceptanceHeading, "## ")]
		violations = append(violations, markdownDerivedViolation(path, line, "Machine-verifiable acceptance must use the canonical typed contract: "+err.Error()))
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
