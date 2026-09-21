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

type shellStateAssignment struct {
	value string
	line  int
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
	versions, legacyPathVariables, assignments := shellStateAssignments(lines)
	legacyProvenanceStates := shellLegacyProvenanceStates(assignments, legacyPathVariables)

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

func shellStateAssignments(lines []string) (map[string]shellVersionedState, map[string]bool, map[string]shellStateAssignment) {
	versions := map[string]shellVersionedState{}
	legacyPathVariables := map[string]bool{}
	assignments := map[string]shellStateAssignment{}
	for index, line := range lines {
		match := shellSimpleAssignmentPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		variable := match[1]
		value := strings.TrimSpace(match[2])
		assignments[variable] = shellStateAssignment{value: value, line: index + 1}
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

func shellLegacyProvenanceStates(assignments map[string]shellStateAssignment, legacyPathVariables map[string]bool) map[string]int {
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
		if shellCaseArmWritesCurrentState(lines[index:], oldVersion, versions, legacyProvenanceStates) {
			return true
		}
	}
	return false
}

func shellCaseArmWritesCurrentState(
	lines []string,
	oldVersion int,
	versions map[string]shellVersionedState,
	legacyProvenanceStates map[string]int,
) bool {
	for _, line := range lines {
		if match := shellWriteStatePattern.FindStringSubmatch(line); len(match) == 2 {
			target := match[1]
			if targetState, ok := versions[target]; ok && targetState.version > oldVersion {
				return true
			}
			if _, ok := legacyProvenanceStates[target]; ok {
				return true
			}
		}
		if strings.Contains(line, ";;") {
			return false
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
	var violations []Violation
	for index, line := range lines {
		if variable, version, ok := shellWrittenStateVersion(line); ok {
			writtenVersions[variable] = shellVersionedState{version: version, line: index + 1}
		}
		variable, expected, ok := shellExpectedStateVersion(line)
		if !ok {
			continue
		}
		written, exists := writtenVersions[variable]
		if !exists || expected <= written.version {
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

func shellWrittenStateVersion(line string) (string, int, bool) {
	if !strings.Contains(line, "printf") || !strings.Contains(line, ">") {
		return "", 0, false
	}
	version, ok := shellVersionLiteral(line)
	if !ok {
		return "", 0, false
	}
	variable := shellRedirectVariable(line)
	return variable, version, variable != ""
}

func shellExpectedStateVersion(line string) (string, int, bool) {
	if !strings.Contains(line, "test") || !strings.Contains(line, "cat") {
		return "", 0, false
	}
	version, ok := shellVersionLiteral(line)
	if !ok {
		return "", 0, false
	}
	match := shellCatVariablePattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return "", 0, false
	}
	return match[1], version, true
}

func shellVersionLiteral(line string) (int, bool) {
	match := shellStateVersionPattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return 0, false
	}
	version, err := strconv.Atoi(match[1])
	return version, err == nil
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
