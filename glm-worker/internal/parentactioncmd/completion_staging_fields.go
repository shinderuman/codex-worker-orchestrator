package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"strings"
)

type completionStagingFields struct {
	Template json.RawMessage
	Semantic map[string]string
}

func parseCompletionStaging(payload []byte, labels map[string]string) (completionStagingFields, error) {
	fields := completionStagingFields{Semantic: map[string]string{}}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(payload), "\n") {
		if line == "" {
			continue
		}
		label, value, ok := strings.Cut(line, ": ")
		if !ok || seen[label] {
			return fields, fmt.Errorf("completion staging has an invalid or duplicate field: %s", label)
		}
		seen[label] = true
		value = strings.TrimSpace(value)
		if value == "" {
			return fields, fmt.Errorf("completion staging field is empty: %s", label)
		}
		if label == "COMMAND_TEMPLATE" {
			fields.Template = json.RawMessage(value)
			continue
		}
		name, ok := labels[label]
		if !ok {
			return fields, fmt.Errorf("completion staging has an undeclared field: %s", label)
		}
		fields.Semantic[name] = value
	}
	if fields.Template == nil {
		return fields, fmt.Errorf("completion staging lacks its machine command template")
	}
	return fields, nil
}

func (fields completionStagingFields) commandObject() (map[string]json.RawMessage, error) {
	var command map[string]json.RawMessage
	if err := json.Unmarshal(fields.Template, &command); err != nil || command == nil {
		return nil, fmt.Errorf("completion machine template is not a JSON object")
	}
	return command, nil
}
