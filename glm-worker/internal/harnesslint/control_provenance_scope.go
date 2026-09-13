package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const controlProvenanceModulePath = "github.com/shinderuman/codex-worker-orchestrator/glm-worker"

func scopedControlProvenanceViolations(root string) ([]Violation, error) {
	applies, err := controlProvenanceModuleMatches(root)
	if err != nil {
		return nil, err
	}
	if !applies {
		return nil, nil
	}
	return controlProvenanceViolations(root)
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
