package harnesslint

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const controlProvenanceModulePath = "github.com/shinderuman/codex-worker-orchestrator/glm-worker"

func scopedControlProvenanceViolations(root string) ([]Violation, error) {
	applies, err := isCodexWorkerOrchestrator(root)
	if err != nil {
		return nil, err
	}
	if !applies {
		return nil, nil
	}
	return controlProvenanceViolations(root)
}

func isCodexWorkerOrchestrator(root string) (bool, error) {
	matchesModule, err := controlProvenanceModuleMatches(root)
	if err != nil || !matchesModule {
		return false, err
	}
	origin, err := controlProvenanceOrigin(root)
	if err != nil {
		return false, err
	}
	return controlProvenanceOriginMatches(origin), nil
}

func controlProvenanceModuleMatches(root string) (bool, error) {
	moduleFile := filepath.Join(root, "glm-worker", "go.mod")
	data, err := os.ReadFile(moduleFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read glm-worker/go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1] == controlProvenanceModulePath, nil
		}
	}
	return false, nil
}

func controlProvenanceOrigin(root string) (string, error) {
	command := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	data, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", fmt.Errorf("read repository origin: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func controlProvenanceOriginMatches(origin string) bool {
	origin = strings.TrimSuffix(strings.TrimSpace(origin), ".git")
	switch origin {
	case "https://github.com/shinderuman/codex-worker-orchestrator",
		"git@github.com:shinderuman/codex-worker-orchestrator",
		"ssh://git@github.com/shinderuman/codex-worker-orchestrator",
		"git://github.com/shinderuman/codex-worker-orchestrator":
		return true
	default:
		return false
	}
}
