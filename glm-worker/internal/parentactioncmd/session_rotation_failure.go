package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const sessionRotationFailUsage = "usage: glm-parent-action rotation-fail <directive-id> <claim-id> --creation-result-json <json>"

func decodeSessionRotationCreationResult(raw string) (state.SessionRotationCreationResult, error) {
	var result state.SessionRotationCreationResult
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("session rotation creation result JSONが不正です: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return result, fmt.Errorf("session rotation creation result JSONに余分な値があります")
		}
		return result, fmt.Errorf("session rotation creation result JSONが不正です: %w", err)
	}
	return result, nil
}
