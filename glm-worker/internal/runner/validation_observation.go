package runner

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type validationSegmentScanner struct {
	command      string
	start        int
	singleQuoted bool
	doubleQuoted bool
	escaped      bool
}

func validationObservationsForToolInput(toolName string, input json.RawMessage) []state.TaskValidationObservation {
	if toolName != bashToolName || len(input) == 0 {
		return nil
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Command == "" {
		return nil
	}
	return validationObservationsForCommand(parsed.Command)
}

func validationObservationsForCommand(command string) []state.TaskValidationObservation {
	segments := splitValidationCommandSegments(command)
	seen := make(map[string]struct{})
	result := make([]state.TaskValidationObservation, 0, 4)
	for _, segment := range segments {
		form := validationFormForSegment(segment)
		if form == "" {
			continue
		}
		if _, ok := seen[form]; ok {
			continue
		}
		seen[form] = struct{}{}
		result = append(result, state.TaskValidationObservation{Form: form})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func splitValidationCommandSegments(command string) []string {
	scanner := validationSegmentScanner{command: command}
	result := make([]string, 0, 4)
	for index := 0; index < len(command); index++ {
		width := scanner.separatorWidth(index)
		if width == 0 {
			continue
		}
		result = append(result, command[scanner.start:index])
		index += width - 1
		scanner.start = index + 1
	}
	result = append(result, command[scanner.start:])
	return result
}

func (s *validationSegmentScanner) separatorWidth(index int) int {
	ch := s.command[index]
	if s.consumeQuoteOrEscape(ch) || s.singleQuoted || s.doubleQuoted {
		return 0
	}
	return validationSeparatorWidth(s.command, index)
}

func (s *validationSegmentScanner) consumeQuoteOrEscape(ch byte) bool {
	if s.escaped {
		s.escaped = false
		return true
	}
	if ch == '\\' && !s.singleQuoted {
		s.escaped = true
		return true
	}
	if ch == '\'' && !s.doubleQuoted {
		s.singleQuoted = !s.singleQuoted
		return true
	}
	if ch == '"' && !s.singleQuoted {
		s.doubleQuoted = !s.doubleQuoted
		return true
	}
	return false
}

func validationSeparatorWidth(command string, index int) int {
	switch command[index] {
	case ';', '\n':
		return 1
	case '&':
		if index+1 < len(command) && command[index+1] == '&' {
			return 2
		}
	case '|':
		if index+1 < len(command) && command[index+1] == '|' {
			return 2
		}
		return 1
	}
	return 0
}

func validationFormForSegment(segment string) string {
	words := strings.Fields(strings.TrimSpace(segment))
	if len(words) == 0 {
		return ""
	}
	for index := range words {
		words[index] = strings.Trim(words[index], "(){}")
	}
	index := validationCommandStart(words)
	if index >= len(words) {
		return ""
	}
	program := filepath.Base(words[index])
	if program == "go" {
		return goValidationForm(words[index+1:])
	}
	switch program {
	case "harnesslint":
		return "harnesslint"
	case "commentlint":
		return "commentlint"
	default:
		return ""
	}
}

func goValidationForm(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "test":
		if slicesContain(args[1:], "-race") {
			return "go-test-race"
		}
		return "go-test"
	case "vet":
		return "go-vet"
	case "build":
		return "go-build"
	default:
		return ""
	}
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validationCommandStart(words []string) int {
	if len(words) == 0 || words[0] != "env" {
		return skipValidationAssignments(words, 0)
	}
	return validationEnvCommandStart(words)
}

func validationEnvCommandStart(words []string) int {
	for index := 1; index < len(words); {
		switch {
		case words[index] == "-u" && index+1 < len(words):
			index += 2
		case words[index] == "--":
			return skipValidationAssignments(words, index+1)
		case strings.HasPrefix(words[index], "--unset="),
			words[index] == "-i",
			words[index] == "--ignore-environment",
			isValidationAssignment(words[index]):
			index++
		default:
			return index
		}
	}
	return len(words)
}

func skipValidationAssignments(words []string, index int) int {
	for index < len(words) && isValidationAssignment(words[index]) {
		index++
	}
	if index < len(words) && words[index] == "command" {
		index++
	}
	return index
}

func isValidationAssignment(word string) bool {
	if strings.HasPrefix(word, "-") {
		return false
	}
	name, _, ok := strings.Cut(word, "=")
	return ok && validValidationEnvName(name)
}

func validValidationEnvName(name string) bool {
	if name == "" {
		return false
	}
	for index, r := range name {
		if !validValidationEnvRune(r, index == 0) {
			return false
		}
	}
	return true
}

func validValidationEnvRune(r rune, first bool) bool {
	if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
		return true
	}
	return !first && r >= '0' && r <= '9'
}

func validationToolResultAttributable(toolName string, input json.RawMessage) bool {
	if toolName != "Bash" || len(input) == 0 {
		return false
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Command == "" {
		return false
	}
	return validationCommandResultAttributable(parsed.Command)
}

func validationCommandResultAttributable(command string) bool {
	if len(validationObservationsForCommand(command)) == 0 {
		return false
	}
	scanner := validationSegmentScanner{command: command}
	for index := 0; index < len(command); index++ {
		if width := scanner.separatorWidth(index); width > 0 {
			return false
		}
		if command[index] == '&' && !scanner.singleQuoted && !scanner.doubleQuoted && validationBackgroundAmpersand(command, index) {
			return false
		}
	}
	return true
}

func validationBackgroundAmpersand(command string, index int) bool {
	if index > 0 && (command[index-1] == '>' || command[index-1] == '<') {
		return false
	}
	if index+1 < len(command) && command[index+1] == '>' {
		return false
	}
	return true
}

func validationObservationsWithToolResult(values []state.TaskValidationObservation, attributable bool, isError bool) []state.TaskValidationObservation {
	if len(values) == 0 {
		return nil
	}
	resultValue := state.ValidationResultUnknown
	if attributable {
		if isError {
			resultValue = state.ValidationResultFail
		} else {
			resultValue = state.ValidationResultPass
		}
	}
	result := make([]state.TaskValidationObservation, len(values))
	copy(result, values)
	for index := range result {
		result[index].Result = resultValue
	}
	return result
}
