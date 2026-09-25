package app

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const parentHandoffRecoverySemanticStateKey = "last-review"

type parentHandoffRecoverySemanticResult struct {
	Availability string          `json:"availability"`
	Authority    string          `json:"authority"`
	StateKey     string          `json:"state_key"`
	CallID       *string         `json:"call_id,omitempty"`
	Packet       json.RawMessage `json:"packet,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

func projectParentHandoffRecoverySemantic(
	st *state.StateStore,
	full parentHandoffOutput,
	recovery parentHandoffRecoveryOutput,
) (map[string]any, error) {
	projected, err := recoveryProjectionObject(recovery)
	if err != nil {
		return nil, err
	}
	if !parentHandoffRecoverySemanticEligible(full) {
		return projected, nil
	}

	semantic := parentHandoffRecoverySemanticResult{
		Availability: "unavailable",
		Authority:    "task-state",
		StateKey:     parentHandoffRecoverySemanticStateKey,
		CallID:       full.LastMaterial.CallID,
	}
	review, err := st.Read(parentHandoffRecoverySemanticStateKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			semantic.Reason = "canonical-review-packet-missing"
		} else {
			semantic.Reason = "canonical-review-packet-unreadable"
		}
		projected["semantic_result"] = semantic
		return projected, nil
	}

	result, err := packet.ParseStructured([]byte(review))
	if err != nil {
		semantic.Reason = "canonical-review-packet-invalid"
		projected["semantic_result"] = semantic
		return projected, nil
	}
	if err := packet.ValidateReviewerResult(result); err != nil {
		semantic.Reason = "canonical-review-packet-invalid"
		projected["semantic_result"] = semantic
		return projected, nil
	}
	if string(result.Status) != full.LastMaterial.PacketStatus ||
		full.ParentReviewOpen == nil || string(result.Status) != *full.ParentReviewOpen {
		semantic.Reason = "canonical-review-packet-status-mismatch"
		projected["semantic_result"] = semantic
		return projected, nil
	}

	machine, err := result.MachineJSON()
	if err != nil {
		semantic.Reason = "canonical-review-packet-encode-failed"
		projected["semantic_result"] = semantic
		return projected, nil
	}
	semantic.Availability = "available"
	semantic.Packet = machine
	projected["semantic_result"] = semantic
	return projected, nil
}

func parentHandoffRecoverySemanticEligible(output parentHandoffOutput) bool {
	material := output.LastMaterial
	if !output.Consistent || output.ParentReviewOpen == nil || material == nil || material.CallID == nil {
		return false
	}
	return material.CallType == state.CallTypeTask &&
		material.Role == string(state.ReviewerRole) &&
		material.Outcome == "success" &&
		material.PacketStatus != "" &&
		material.PacketStatus == *output.ParentReviewOpen
}

func recoveryProjectionObject(recovery parentHandoffRecoveryOutput) (map[string]any, error) {
	raw, err := json.Marshal(recovery)
	if err != nil {
		return nil, err
	}
	var projected map[string]any
	if err := json.Unmarshal(raw, &projected); err != nil {
		return nil, err
	}
	return projected, nil
}
