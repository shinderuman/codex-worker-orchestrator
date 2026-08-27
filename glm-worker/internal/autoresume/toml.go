package autoresume

import (
	"fmt"
	"strings"
)

func parseAutomationTOML(data []byte) (AutomationTOML, error) {
	values := make(map[string]string)
	bare := make(map[string]bool)
	for i, rawLine := range strings.Split(string(data), "\n") {
		key, value, isBare, skip, err := parseAutomationLine(rawLine, i+1)
		if err != nil {
			return AutomationTOML{}, err
		}
		if skip {
			continue
		}
		if _, exists := values[key]; exists {
			return AutomationTOML{}, fmt.Errorf("duplicate key %q", key)
		}
		values[key] = value
		bare[key] = isBare
	}
	if err := validateAutomationFields(values, bare); err != nil {
		return AutomationTOML{}, err
	}
	return AutomationTOML{
		ID:             values["id"],
		Name:           values["name"],
		Status:         values["status"],
		Rrule:          values["rrule"],
		TargetThreadID: values["target_thread_id"],
		Prompt:         values["prompt"],
	}, nil
}

func parseAutomationLine(rawLine string, lineNumber int) (string, string, bool, bool, error) {
	line := strings.TrimSpace(rawLine)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, true, nil
	}
	eq := strings.Index(line, "=")
	if eq < 0 {
		return "", "", false, false, fmt.Errorf("line %d: key=value separator missing", lineNumber)
	}
	key := strings.TrimSpace(line[:eq])
	if key == "" {
		return "", "", false, false, fmt.Errorf("line %d: empty key", lineNumber)
	}
	value := strings.TrimSpace(line[eq+1:])
	if !strings.HasPrefix(value, `"`) {
		return key, value, true, false, nil
	}
	parsed, err := parseBasicString(value)
	if err != nil {
		return "", "", false, false, fmt.Errorf("key %q: %w", key, err)
	}
	return key, parsed, false, false, nil
}

func validateAutomationFields(values map[string]string, bare map[string]bool) error {
	for _, field := range []string{"id", "name", "status", "rrule", "target_thread_id"} {
		if _, exists := values[field]; !exists {
			return fmt.Errorf("missing required field %q", field)
		}
		if bare[field] {
			return fmt.Errorf("field %q must be a quoted string, got bare value", field)
		}
	}
	return nil
}

func parseBasicString(value string) (string, error) {
	if !strings.HasPrefix(value, `"`) {
		return "", fmt.Errorf("not a basic string")
	}
	var sb strings.Builder
	for i := 1; i < len(value); {
		if value[i] == '\\' {
			next, consumed, err := parseBasicEscape(value, i)
			if err != nil {
				return "", err
			}
			sb.WriteString(next)
			i = consumed
			continue
		}
		if value[i] == '"' {
			if err := validateBasicStringRemainder(value[i+1:]); err != nil {
				return "", err
			}
			return sb.String(), nil
		}
		sb.WriteByte(value[i])
		i++
	}
	return "", fmt.Errorf("unterminated string")
}

func parseBasicEscape(value string, index int) (string, int, error) {
	if index+1 >= len(value) {
		return "", index, fmt.Errorf("unterminated escape sequence")
	}
	next := value[index+1]
	switch next {
	case 'n':
		return "\n", index + 2, nil
	case 't':
		return "\t", index + 2, nil
	case 'r':
		return "\r", index + 2, nil
	case '"':
		return `"`, index + 2, nil
	case '\\':
		return `\`, index + 2, nil
	default:
		return string([]byte{'\\', next}), index + 2, nil
	}
}

func validateBasicStringRemainder(remainder string) error {
	trimmed := strings.TrimSpace(remainder)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}
	return fmt.Errorf("trailing content after string close: %q", trimmed)
}
