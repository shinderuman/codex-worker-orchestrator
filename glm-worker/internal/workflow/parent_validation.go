package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const finalValidationNoneMarker = `{"None":true}`

func (w *Workflow) convergeParentValidation(result packet.Result) (packet.Result, error) {
	if result.Status != packet.StatusImplemented {
		return result, nil
	}
	if err := w.state.ContinueAfterWorkerResult(); err != nil {
		return packet.Result{}, err
	}
	request, err := w.recordFinalValidationObligation(result.ParentValidationRequest())
	if err != nil {
		return packet.Result{}, err
	}
	result.SetParentValidationRequest(request)
	result.ParentValidationEvidence = nil
	return result, nil
}

func (w *Workflow) recordFinalValidationObligation(request *packet.ParentValidationRequest) (*packet.ParentValidationRequest, error) {
	existing, err := w.state.Read(state.FinalValidationObligationKey)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && existing != finalValidationNoneMarker {
		previous, err := decodeFinalValidationObligation(existing)
		if err != nil {
			return nil, err
		}
		if request != nil && !sameParentValidationRequest(previous, *request) {
			return nil, &WorkerError{Phase: "parent-validation", Message: fmt.Sprintf(
				"worker changed the final boundary validation obligation from %s@%s to %s@%s",
				previous.Form, previous.WorkingDir, request.Form, request.WorkingDir)}
		}
		return &previous, nil
	}
	stored := finalValidationNoneMarker
	if request != nil {
		encoded, err := json.Marshal(*request)
		if err != nil {
			return nil, err
		}
		stored = string(encoded)
	}
	if existing != stored {
		if err := w.state.Write(state.FinalValidationObligationKey, stored); err != nil {
			return nil, err
		}
	}
	return request, nil
}

func sameParentValidationRequest(left, right packet.ParentValidationRequest) bool {
	return left.Form == right.Form && left.WorkingDir == right.WorkingDir
}

func decodeFinalValidationObligation(raw string) (packet.ParentValidationRequest, error) {
	var previous packet.ParentValidationRequest
	if err := json.Unmarshal([]byte(raw), &previous); err != nil {
		return previous, fmt.Errorf("stored final validation obligation is invalid: %w", err)
	}
	if (previous.Form != packet.ParentValidationGoTest && previous.Form != packet.ParentValidationGoTestRace) || previous.WorkingDir == "" {
		return previous, fmt.Errorf("stored final validation obligation is incomplete")
	}
	return previous, nil
}
