package runner

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func validationObservationsForToolInput(toolName string, input json.RawMessage) []state.TaskValidationObservation {
	if toolName != "Bash" || len(input) == 0 {
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
	var segments []string
	start := 0
	singleQuoted := false
	doubleQuoted := false
	escaped := false
	for index := 0; index < len(command); index++ {
		ch := command[index]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !singleQuoted {
			escaped = true
			continue
		}
		switch ch {
		case '\'':
			if !doubleQuoted {
				singleQuoted = !singleQuoted
			}
		case '"':
			if !singleQuoted {
				doubleQuoted = !doubleQuoted
			}
		}
		if singleQuoted || doubleQuoted {
			continue
		}

		separatorWidth := 0
		switch ch {
		case ';', '\n':
			separatorWidth = 1
		case '&':
			if index+1 < len(command) && command[index+1] == '&' {
				separatorWidth = 2
			}
		case '|':
			separatorWidth = 1
			if index+1 < len(command) && command[index+1] == '|' {
				separatorWidth = 2
			}
		}
		if separatorWidth == 0 {
			continue
		}
		segments = append(segments, command[start:index])
		index += separatorWidth - 1
		start = index + 1
	}
	segments = append(segments, command[start:])
	return segments
}

func validationFormForSegment(segment string) string {
	words := strings.Fields(strings.TrimSpace(segment))
	if len(words) == 0 {
		return ""
	}
	for i := range words {
		words[i] = strings.Trim(words[i], "(){}")
	}
	index := validationCommandStart(words)
	if index >= len(words) {
		return ""
	}
	program := filepath.Base(words[index])
	args := words[index+1:]
	switch program {
	case "go":
		if len(args) == 0 {
			return ""
		}
		switch args[0] {
		case "test":
			for _, arg := range args[1:] {
				if arg == "-race" {
					return "go-test-race"
				}
			}
			return "go-test"
		case "vet":
			return "go-vet"
		case "build":
			return "go-build"
		}
	case "harnesslint":
		return "harnesslint"
	case "commentlint":
		return "commentlint"
	}
	return ""
}

func validationCommandStart(words []string) int {
	index := 0
	if index < len(words) && words[index] == "env" {
		index++
		for index < len(words) {
			switch {
			case words[index] == "-u" && index+1 < len(words):
				index += 2
			case words[index] == "--":
				index++
				return skipValidationAssignments(words, index)
			case strings.HasPrefix(words[index], "--unset="),
				words[index] == "-i",
				words[index] == "--ignore-environment":
				index++
			case isValidationAssignment(words[index]):
				index++
			default:
				return index
			}
		}
		return index
	}
	return skipValidationAssignments(words, index)
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
	if !ok || name == "" {
		return false
	}
	for index, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
