package harnesslint

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var controlProjectionPattern = regexp.MustCompile("`control:([a-z][a-z0-9-]*)`")

func controlProjectionViolations(root string) ([]Violation, error) {
	registryData, err := readRegularFile(root, controlProvenanceRegistryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read control projection registry: %w", err)
	}
	registry, err := decodeControlProvenanceRegistry(registryData)
	if err != nil {
		return nil, nil
	}
	classifications := make(map[string]string, len(registry.Controls))
	for _, control := range registry.Controls {
		classifications[control.ID] = control.Classification
	}
	paths, err := repositoryPaths(root)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, path := range paths {
		if !isControlProjectionSurface(path) {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, controlProjectionPathViolations(path, data, classifications)...)
	}
	return violations, nil
}

func isControlProjectionSurface(path string) bool {
	if path == "AGENTS.md" || path == "IMPLEMENTATION_RULES.md" || path == "codex/AGENTS.md" {
		return true
	}
	if !strings.HasSuffix(path, ".md") {
		return false
	}
	return strings.HasPrefix(path, "codex/instructions/") || strings.HasPrefix(path, "codex/glm-worker/prompts/")
}

func controlProjectionPathViolations(path string, data []byte, classifications map[string]string) []Violation {
	var violations []Violation
	for index, line := range bytes.Split(data, []byte("\n")) {
		for _, match := range controlProjectionPattern.FindAllSubmatch(line, -1) {
			id := string(match[1])
			classification, ok := classifications[id]
			if !ok {
				violations = append(violations, controlProjectionViolation(path, index+1, fmt.Sprintf("control projection %q has no provenance registry entry", id)))
				continue
			}
			if classification != controlClassificationMachine {
				violations = append(violations, controlProjectionViolation(path, index+1, fmt.Sprintf("control projection %q targets %q instead of machine-enforced", id, classification)))
			}
		}
	}
	return violations
}

func controlProjectionViolation(path string, line int, message string) Violation {
	return Violation{
		Rule:    "control-provenance-projection",
		Path:    path,
		Line:    line,
		Column:  1,
		Message: message,
	}
}
