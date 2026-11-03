package taskcontract

import (
	"fmt"
	"strings"
)

type ExecutionAuthority struct {
	Present bool
	Mode    string
}

const (
	ExecutionAuthorityHeading = "## Execution authority"

	ExecutionAuthorityModeParentOnly = "parent-only"
	ExecutionAuthorityModeGLM        = "glm"
)

func ParseExecutionAuthority(content []byte) (ExecutionAuthority, error) {
	lines := strings.Split(string(content), "\n")
	headingAt, err := findUniqueTaskSection(lines, ExecutionAuthorityHeading)
	if err != nil {
		return ExecutionAuthority{}, err
	}
	if headingAt < 0 {
		return ExecutionAuthority{}, nil
	}
	values, err := parseExecutionAuthorityValues(lines, headingAt)
	if err != nil {
		return ExecutionAuthority{}, err
	}
	mode, ok := values["mode"]
	if !ok {
		return ExecutionAuthority{}, fmt.Errorf("%s節にmodeがありません(parent-only/glm)", ExecutionAuthorityHeading)
	}
	switch mode {
	case ExecutionAuthorityModeParentOnly, ExecutionAuthorityModeGLM:
	default:
		return ExecutionAuthority{}, fmt.Errorf("%s節のmode %qは未知です(parent-only/glm)", ExecutionAuthorityHeading, mode)
	}
	return ExecutionAuthority{Present: true, Mode: mode}, nil
}

func parseExecutionAuthorityValues(lines []string, headingAt int) (map[string]string, error) {
	values := map[string]string{}
	fence := 0
	for index := headingAt + 1; index < len(lines); index++ {
		line := lines[index]
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key != "mode" {
			return nil, fmt.Errorf("%s節の%d行目 %qがmode: value形式ではありません", ExecutionAuthorityHeading, index+1, trimmed)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("%s節のkey %qが重複しています", ExecutionAuthorityHeading, key)
		}
		if value == "" {
			return nil, fmt.Errorf("%s節のkey %qのvalueが空です", ExecutionAuthorityHeading, key)
		}
		values[key] = value
	}
	return values, nil
}
