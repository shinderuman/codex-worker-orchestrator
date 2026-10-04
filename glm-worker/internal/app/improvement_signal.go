package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

type parentHandoffImprovementSignal struct {
	Signal     state.ImprovementSignal `json:"signal"`
	ActionSpec parentHandoffActionSpec `json:"action_spec"`
}

const invalidPacketOutcome = "invalid_packet"

func projectImprovementSignal(output *parentHandoffOutput) *parentHandoffImprovementSignal {
	if output == nil || output.Controller == nil {
		return nil
	}
	return projectCanonicalImprovementSignal(improvementSignalFromMaterial(output.LastMaterial))
}

func projectRecoveryImprovementSignal(output *parentHandoffRecoveryOutput) *parentHandoffImprovementSignal {
	if output == nil || output.Controller == nil {
		return nil
	}
	return projectCanonicalImprovementSignal(improvementSignalFromRecoveryMaterial(output.LastMaterial))
}

func projectCanonicalImprovementSignal(signal *state.ImprovementSignal) *parentHandoffImprovementSignal {
	if signal == nil {
		return nil
	}
	spec, ok := parentActionSpec("controller-semantic", nil)
	if !ok {
		return nil
	}
	return &parentHandoffImprovementSignal{Signal: *signal, ActionSpec: spec}
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
	return &state.ImprovementSignal{Kind: state.ImprovementSignalInvalidPacket, Count: 1, SourceCallID: *callID, Reason: reason}
}
