package harnesslint

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	controlProjectionMarkerPattern = regexp.MustCompile("`control:([^`]*)`")
	controlProjectionIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

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
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		pathSet[path] = struct{}{}
	}
	var violations []Violation
	violations = append(violations, controlProjectionProcedureGuardRegistryViolations(registry.Controls, pathSet)...)
	for _, path := range paths {
		if !isControlProjectionSurface(path) {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, controlProjectionPathViolations(path, data, classifications)...)
		violations = append(violations, controlProjectionProcedureGuardViolations(path, data, registry.Controls)...)
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
		for _, match := range controlProjectionMarkerPattern.FindAllSubmatch(line, -1) {
			id := string(match[1])
			if !controlProjectionIDPattern.MatchString(id) {
				violations = append(violations, controlProjectionViolation(path, index+1, fmt.Sprintf("control projection %q has invalid id syntax", id)))
				continue
			}
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

func controlProjectionProcedureGuardRegistryViolations(controls []controlProvenanceControl, paths map[string]struct{}) []Violation {
	var violations []Violation
	for _, control := range controls {
		for _, guard := range control.ProjectionGuards {
			if control.Classification != controlClassificationMachine {
				violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q targets %q instead of machine-enforced", control.ID, control.Classification)))
				continue
			}
			if !isControlProjectionSurface(guard.Path) {
				violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q targets non-model-facing surface %q", control.ID, guard.Path)))
				continue
			}
			if _, ok := paths[guard.Path]; !ok {
				violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q projection surface %q is missing", control.ID, guard.Path)))
				continue
			}
			if len(guard.ForbiddenTokens) == 0 {
				violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q for %q has no forbidden tokens", control.ID, guard.Path)))
				continue
			}
			seenTokens := make(map[string]struct{}, len(guard.ForbiddenTokens))
			for _, token := range guard.ForbiddenTokens {
				if strings.TrimSpace(token) == "" {
					violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q for %q has an empty forbidden token", control.ID, guard.Path)))
					continue
				}
				if _, duplicate := seenTokens[token]; duplicate {
					violations = append(violations, controlProjectionViolation(controlProvenanceRegistryPath, 1, fmt.Sprintf("procedure guard %q for %q repeats forbidden token %q", control.ID, guard.Path, token)))
					continue
				}
				seenTokens[token] = struct{}{}
			}
		}
	}
	return violations
}

func controlProjectionProcedureGuardViolations(path string, data []byte, controls []controlProvenanceControl) []Violation {
	var violations []Violation
	for _, control := range controls {
		if control.Classification != controlClassificationMachine {
			continue
		}
		for _, guard := range control.ProjectionGuards {
			if guard.Path != path {
				continue
			}
			marker := []byte("`control:" + control.ID + "`")
			if !bytes.Contains(data, marker) {
				violations = append(violations, controlProjectionViolation(path, 1, fmt.Sprintf("procedure guard %q is missing its compact control projection", control.ID)))
				continue
			}
			for _, token := range guard.ForbiddenTokens {
				index := bytes.Index(data, []byte(token))
				if index < 0 {
					continue
				}
				line := bytes.Count(data[:index], []byte("\n")) + 1
				violations = append(violations, controlProjectionViolation(path, line, fmt.Sprintf("control projection %q reintroduces machine-owned procedure token %q", control.ID, token)))
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
