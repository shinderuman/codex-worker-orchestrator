package repositoryproject

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type ObservationCapabilityPolicy struct {
	Status          string
	Admitted        bool
	AuthorityDigest string
}

func EvaluateObservationCapability(content []byte) (ObservationCapabilityPolicy, error) {
	declaration, err := taskcontract.ParseExternalFeasibility(content)
	if err != nil {
		return ObservationCapabilityPolicy{}, fmt.Errorf("ACTIVE taskのExternal feasibility宣言を受理できません: %w", err)
	}
	digest := sha256.Sum256(content)
	policy := ObservationCapabilityPolicy{
		Status:          declaration.Status,
		AuthorityDigest: hex.EncodeToString(digest[:]),
	}
	policy.Admitted = declaration.Status == taskcontract.StatusPoC || declaration.Status == taskcontract.StatusObservation
	return policy, nil
}
