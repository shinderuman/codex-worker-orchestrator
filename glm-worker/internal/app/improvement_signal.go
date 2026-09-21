package app

import (
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	improvementSignalKindParameter   = "signal-kind"
	improvementSignalCountParameter  = "signal-count"
	improvementSignalCallIDParameter = "source-call-id"
	improvementSignalReasonParameter = "reason"
	invalidPacketOutcome             = "invalid_packet"
)

func projectImprovementSignal(output *parentHandoffOutput) {
	if output == nil {
		return
	}
	signal := improvementSignalFromMaterial(output.LastMaterial)
	if signal == nil {
		return
	}
	projectImprovementSignalAction(&output.RequiredAction, &output.AllowedActions, &output.RequiredActionParameters, *signal)
}

func projectRecoveryImprovementSignal(output *parentHandoffRecoveryOutput) {
	if output == nil {
		return
	}
	signal := improvementSignalFromRecoveryMaterial(output.LastMaterial)
	if signal == nil {
		return
	}
	projectImprovementSignalAction(&output.RequiredAction, &output.AllowedActions, &output.RequiredActionParameters, *signal)
}

func projectImprovementSignalAction(required **string, allowed *[]string, parameters *map[string]string, signal state.ImprovementSignal) {
	action := string(state.ParentActionImprovementDisposition)
	*required = &action
	*allowed = []string{action}
	*parameters = improvementSignalParameters(signal)
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
	reason := rejectReason
	if reason == "" {
		reason = state.ImprovementSignalInvalidPacket
	}
	sourceCallID := ""
	if callID != nil {
		sourceCallID = *callID
	}
	return &state.ImprovementSignal{
		Kind:         state.ImprovementSignalInvalidPacket,
		Count:        1,
		SourceCallID: sourceCallID,
		Reason:       reason,
	}
}
