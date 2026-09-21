package harnesslint

import (
	"regexp"
	"strconv"
	"strings"
)

type shellVersionedState struct {
	version int
	line    int
}

var (
	shellSimpleAssignmentPattern = regexp.MustCompile(`^[\t ]*([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	shellStateVersionPattern     = regexp.MustCompile(`\bversion=([0-9]+)\b`)
	shellStateKindPattern        = regexp.MustCompile(`^[\t ]*"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?\)[\t ]*state_kind=([A-Za-z0-9_-]+)`)
	shellWriteStatePattern       = regexp.MustCompile(`\bwrite_state[\t ]+"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?`)
	shellVariablePattern         = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	shellCatVariablePattern      = regexp.MustCompile(`\bcat[\t ]+"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?`)
)

func scanForwardOnlyShellStateCompatibility(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range paths {
		if forwardOnlyFixturePath(path) || !isShellPath(path) {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, forwardOnlyShellStateCompatibilityViolations(path, data)...)
	}
	return violations, nil
}

func forwardOnlyShellStateCompatibilityViolations(path string, data []byte) []Violation {
	lines := strings.Split(string(data), "\n")
	versions, legacyPathVariables, stateAssignments := shellStateAssignments(lines)
	legacyProvenanceStates := shellLegacyProvenanceStates(stateAssignments, legacyPathVariables)

	var violations []Violation
	for variable, line := range legacyProvenanceStates {
		if shellWritesStateVariable(lines, variable) {
			violations = append(violations, shellStateCompatibilityViolation(
				path,
				line,
				"old hook-layout provenance must not be persisted as current installer ownership state",
			))
		}
	}

	currentVersion := highestShellStateVersion(versions)
	for variable, state := range versions {
		if state.version >= currentVersion {
			continue
		}
		kind := shellStateKindForVariable(lines, variable)
		if kind == "" || !shellStateKindWritesCurrentState(lines, kind, state.version, versions, legacyProvenanceStates) {
			continue
		}
		violations = append(violations, shellStateCompatibilityViolation(
			path,
			state.line,
			"old hook ownership state must not be migrated or promoted into current installer state",
		))
	}

	violations = append(violations, shellOldStateAcceptanceTestViolations(path, lines)...)
	return violations
}

func shellStateAssignments(lines []string) (map[string]shellVersionedState, map[string]bool, map[string]struct {
	value string
	line  int
}) {
	versions := map[string]shellVersionedState{}
	legacyPathVariables := map[string]bool{}
	assignments := map[string]struct {
		value string
		line  int
	}{}
	for index, line := range lines {
		match := shellSimpleAssignmentPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		variable := match[1]
		value := strings.TrimSpace(match[2])
		assignments[variable] = struct {
			value string
			line  int
		}{value: value, line: index + 1}
		if shellLiteralValue(value) == ".githooks" {
			legacyPathVariables[variable] = true
		}
		versionMatch := shellStateVersionPattern.FindStringSubmatch(value)
		if len(versionMatch) != 2 {
			continue
		}
		version, err := strconv.Atoi(versionMatch[1])
		if err != nil {
			continue
		}
		versions[variable] = shellVersionedState{version: version, line: index + 1}
	}
	return versions, legacyPathVariables, assignments
}

func shellLiteralValue(value string) string {
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		return value[1 : len(value)-1]
	}
	return value
}

func shellLegacyProvenanceStates(assignments map[string]struct {
	value string
	line  int
}, legacyPathVariables map[string]bool) map[string]int {
	result := map[string]int{}
	for variable, assignment := range assignments {
		for legacyPathVariable := range legacyPathVariables {
			if shellValueUsesLegacyBaseline(assignment.value, legacyPathVariable) {
				result[variable] = assignment.line
				break
			}
		}
	}
	return result
}

func shellValueUsesLegacyBaseline(value, variable string) bool {
	for _, field := range []string{"baseline", "source"} {
		if strings.Contains(value, field+"=$"+variable) || strings.Contains(value, field+"=${"+variable+"}") {
			return true
		}
	}
	return false
}

func shellWritesStateVariable(lines []string, variable string) bool {
	for _, line := range lines {
		match := shellWriteStatePattern.FindStringSubmatch(line)
		if len(match) == 2 && match[1] == variable {
			return true
		}
	}
	return false
}

func highestShellStateVersion(states map[string]shellVersionedState) int {
	current := 0
	for _, state := range states {
		if state.version > current {
			current = state.version
		}
	}
	return current
}

func shellStateKindForVariable(lines []string, variable string) string {
	for _, line := range lines {
		match := shellStateKindPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 3 && match[1] == variable {
			return match[2]
		}
	}
	return ""
}

func shellStateKindWritesCurrentState(
	lines []string,
	kind string,
	oldVersion int,
	versions map[string]shellVersionedState,
	legacyProvenanceStates map[string]int,
) bool {
	for index, line := range lines {
		if !shellCaseArmContainsKind(line, kind) {
			continue
		}
		for armIndex := index; armIndex < len(lines); armIndex++ {
			armLine := lines[armIndex]
			if match := shellWriteStatePattern.FindStringSubmatch(armLine); len(match) == 2 {
				target := match[1]
				if targetState, ok := versions[target]; ok && targetState.version > oldVersion {
					return true
				}
				if _, ok := legacyProvenanceStates[target]; ok {
					return true
				}
			}
			if strings.Contains(armLine, ";;") {
				break
			}
		}
	}
	return false
}

func shellCaseArmContainsKind(line, kind string) bool {
	trimmed := strings.TrimSpace(line)
	close := strings.Index(trimmed, ")")
	if close < 0 || strings.HasPrefix(trimmed, "case ") {
		return false
	}
	for _, candidate := range strings.Split(trimmed[:close], "|") {
		if strings.Trim(strings.TrimSpace(candidate), `"'`) == kind {
			return true
		}
	}
	return false
}

func shellOldStateAcceptanceTestViolations(path string, lines []string) []Violation {
	writtenVersions := map[string]shellVersionedState{}
	for index, line := range lines {
		if !strings.Contains(line, "printf") || !strings.Contains(line, ">") {
			continue
		}
		versionMatch := shellStateVersionPattern.FindStringSubmatch(line)
		if len(versionMatch) != 2 {
			continue
		}
		variable := shellRedirectVariable(line)
		if variable == "" {
			continue
		}
		version, err := strconv.Atoi(versionMatch[1])
		if err != nil {
			continue
		}
		writtenVersions[variable] = shellVersionedState{version: version, line: index + 1}
	}

	var violations []Violation
	for index, line := range lines {
		if !strings.Contains(line, "test") || !strings.Contains(line, "cat") {
			continue
		}
		versionMatch := shellStateVersionPattern.FindStringSubmatch(line)
		catMatch := shellCatVariablePattern.FindStringSubmatch(line)
		if len(versionMatch) != 2 || len(catMatch) != 2 {
			continue
		}
		written, ok := writtenVersions[catMatch[1]]
		if !ok {
			continue
		}
		expected, err := strconv.Atoi(versionMatch[1])
		if err != nil || expected <= written.version {
			continue
		}
		violations = append(violations, shellStateCompatibilityViolation(
			path,
			index+1,
			"tests must not guarantee upgrading old hook ownership state into a newer state version",
		))
	}
	return violations
}

func shellRedirectVariable(line string) string {
	redirect := strings.LastIndex(line, ">")
	if redirect < 0 {
		return ""
	}
	match := shellVariablePattern.FindStringSubmatch(line[redirect+1:])
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func shellStateCompatibilityViolation(path string, line int, message string) Violation {
	return Violation{Rule: forwardOnlyCompatibilityRule, Path: path, Line: line, Column: 1, Message: message}
}
