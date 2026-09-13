package harnesslint

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type controlProjectionProcedureGuard struct {
	ControlID       string
	Path            string
	ForbiddenTokens []string
}

var (
	controlProjectionMarkerPattern = regexp.MustCompile("`control:([^`]*)`")
	controlProjectionIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

var controlProjectionProcedureGuards = []controlProjectionProcedureGuard{
	{
		ControlID: "external-feasibility-admission",
		Path:      "codex/instructions/feasibility-gate.md",
		ForbiddenTokens: []string{
			"external_feasibility_missing",
			"external_feasibility_malformed",
			"external_feasibility_unverified",
		},
	},
	{
		ControlID: "parent-evidence-projection-dedup",
		Path:      "codex/instructions/glm-parent-evidence.md",
		ForbiddenTokens: []string{
			"duplicate_parent_projection",
			"--known-content-sha256",
		},
	},
	{
		ControlID: "repo-search-exhaustive-activation",
		Path:      "codex/instructions/glm-repo-search.md",
		ForbiddenTokens: []string{
			"EXHAUSTIVE_SEARCH_REQUIRED: true",
			"duplicate_parent_projection",
		},
	},
	{
		ControlID: "stop-isolate-park-lifecycle",
		Path:      "codex/instructions/glm-stop-isolate.md",
		ForbiddenTokens: []string{
			"stop_endpoint_absent",
			"stop_endpoint_stale",
			"interrupted_cleanup_residual",
			"stop-worktree.patch",
			"stop-index.patch",
		},
	},
	{
		ControlID: "orphan-watch-terminalization",
		Path:      "codex/instructions/glm-watch-orphan-terminal.md",
		ForbiddenTokens: []string{
			`status: "orphan-terminal"`,
			`required_action: "none"`,
		},
	},
	{
		ControlID: "packet-schema-result",
		Path:      "codex/glm-worker/prompts/WORKER.md",
		ForbiddenTokens: []string{
			"6 KiB",
			"1536 bytes",
			"parent_validation_working_dir",
		},
	},
	{
		ControlID: "packet-schema-result",
		Path:      "codex/glm-worker/prompts/REVIEWER.md",
		ForbiddenTokens: []string{
			"6 KiB",
			"1536 bytes",
		},
	},
	{
		ControlID: "parent-action-staging-admission",
		Path:      "codex/instructions/task-request-boundary.md",
		ForbiddenTokens: []string{
			"start-milestones <token>",
			"revise-milestones <token>",
			`fresh_worker":true`,
		},
	},
}

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
		violations = append(violations, controlProjectionProcedureGuardViolations(path, data, classifications)...)
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

func controlProjectionProcedureGuardViolations(path string, data []byte, classifications map[string]string) []Violation {
	var violations []Violation
	for _, guard := range controlProjectionProcedureGuards {
		if guard.Path != path {
			continue
		}
		classification, ok := classifications[guard.ControlID]
		if !ok {
			violations = append(violations, controlProjectionViolation(path, 1, fmt.Sprintf("procedure guard %q has no provenance registry entry", guard.ControlID)))
			continue
		}
		if classification != controlClassificationMachine {
			violations = append(violations, controlProjectionViolation(path, 1, fmt.Sprintf("procedure guard %q targets %q instead of machine-enforced", guard.ControlID, classification)))
			continue
		}
		marker := []byte("`control:" + guard.ControlID + "`")
		if !bytes.Contains(data, marker) {
			violations = append(violations, controlProjectionViolation(path, 1, fmt.Sprintf("procedure guard %q is missing its compact control projection", guard.ControlID)))
			continue
		}
		for _, token := range guard.ForbiddenTokens {
			index := bytes.Index(data, []byte(token))
			if index < 0 {
				continue
			}
			line := bytes.Count(data[:index], []byte("\n")) + 1
			violations = append(violations, controlProjectionViolation(path, line, fmt.Sprintf("control projection %q reintroduces machine-owned procedure token %q", guard.ControlID, token)))
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
