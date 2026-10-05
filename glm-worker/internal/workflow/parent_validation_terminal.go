package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func validateParentValidationTerminalPass(record qualitygate.RunRecord) error {
	if record.Status != qualitygate.StatusPass {
		return nil
	}
	if err := qualitygate.VerifyTerminalPass(record); err != nil {
		return &WorkerError{Phase: "parent-validation", Message: fmt.Sprintf("parent validation PASS evidenceがterminal integrityを満たしません: %v", err)}
	}
	return nil
}
