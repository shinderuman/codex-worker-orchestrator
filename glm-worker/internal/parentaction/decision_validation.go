package parentaction

import (
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
)

func validateDecisionPayloadSemantics(parts [][]byte) error {
	lines := make([]string, len(parts))
	for index := range parts {
		lines[index] = string(parts[index])
	}
	_, err := executionunit.Parse(strings.Join(lines, "\n"))
	return err
}
