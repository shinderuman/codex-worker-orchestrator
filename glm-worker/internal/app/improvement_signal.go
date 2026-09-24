package app

import (
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffImprovementSignal struct {
	Signal     state.ImprovementSignal `json:"signal"`
	ActionSpec parentHandoffActionSpec `json:"action_spec"`
}

const (
	improvementSignalKindParameter   = parentactiongrammar.SignalKindParameter
	improvementSignalCountParameter  = "signal-count"
	improvementSignalCallIDParameter = parentactiongrammar.SourceCallIDParameter
	improvementSignalReasonParameter = "reason"
	invalidPacketOutcome             = "invalid_packet"
)

func projectImprovementSignal(output *parentHandoffOutput) *parentHandoffImprovementSignal {
	if output == nil {
		return nil
	}
	return improvementSignalAdvisory(improvementSignalFromMaterial(output.LastMaterial))
}

func projectRecoveryImprovementSignal(output *parentHandoffRecoveryOutput) *parentHandoffImprovementSignal {
	if output == nil {
		return nil
	}
	return improvementSignalAdvisory(improvementSignalFromRecoveryMaterial(output.LastMaterial))
}

func improvementSignalAdvisory(signal *state.ImprovementSignal) *parentHandoffImprovementSignal {
	if signal == nil {
		return nil
	}
	spec, ok := parentactiongrammar.Project(string(state.ParentActionImprovementDisposition), improvementSignalParameters(*signal))
	if !ok {
		return nil
	}
	return &parentHandoffImprovementSignal{
		Signal:     *signal,
		ActionSpec: spec,
	}
}

func improvementSignalParameters(signal state.ImprovementSignal) map[string]string {
	parameters := map[string]string{
		improvementSignalKindParameter:  signal.Kind,
		improvementSignalCountParameter: strconv.Itoa(signal.Count),
	}
	if signal.SourceCallID != "" {
		parameters[improvementSignalCallIDParameter] = signal.SourceCallID
	}
	if signal.Reason != "" {
		parameters[improvementSignalReasonParameter] = signal.Reason
	}
	return parameters
}

func improvementSignalFromMaterial(material *parentHandoffMaterial) *state.ImprovementSignal {
	if material == nil || material.Outcome != invalidPacketOutcome {
		return nil
	}
	return invalidPacketImprovementSignal(material.CallID, material.PacketRejectReason)
}

func improvementSignalFromRecoveryMaterial(material *parentHandoffRecoveryMaterial) *state.ImprovementSignal {
	if material == nil || material.Outcome != invalidPacketOutcome {
		return nil
	}
	return invalidPacketImprovementSignal(material.CallID, material.PacketRejectReason)
}

func invalidPacketImprovementSignal(callID *string, rejectReason string) *state.ImprovementSignal {
	if callID == nil || *callID == "" {
		return nil
	}
	reason := rejectReason
	if reason == "" {
		reason = state.ImprovementSignalInvalidPacket
	}
	return &state.ImprovementSignal{
		Kind:         state.ImprovementSignalInvalidPacket,
		Count:        1,
		SourceCallID: *callID,
		Reason:       reason,
	}
}
