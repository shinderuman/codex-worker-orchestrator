package packet

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func ValidateReviewTargetProof(repoRoot string, targets []string) error {
	if repoRoot == "" {
		return fmt.Errorf("review target repository root is unavailable")
	}
	for _, raw := range targets {
		target, err := reviewtarget.ParseTarget(raw)
		if err != nil {
			return newConstraintError("targets-review-shape", err.Error())
		}
		if err := reviewtarget.ValidateProofAddressable(repoRoot, target); err != nil {
			var proof *reviewtarget.ProofError
			if errors.As(err, &proof) {
				return newConstraintError("targets-review-proof:"+proof.Code, err.Error())
			}
			return err
		}
	}
	return nil
}
