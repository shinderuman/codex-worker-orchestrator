package repositoryproject

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"

type ObservationCapabilityPolicy struct {
	Status   string
	Admitted bool
}

func EvaluateObservationCapability(content []byte) (ObservationCapabilityPolicy, error) {
	declaration, err := taskcontract.ParseExternalFeasibility(content)
	if err != nil {
		return ObservationCapabilityPolicy{}, err
	}
	return ObservationCapabilityPolicy{
		Status: declaration.Status,
		Admitted: declaration.Status == taskcontract.StatusPoC || declaration.Status == taskcontract.StatusObservation,
	}, nil
}
