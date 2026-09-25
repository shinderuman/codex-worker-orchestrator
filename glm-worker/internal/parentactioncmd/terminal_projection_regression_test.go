package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestTerminalProjectionPreservesNoArtifactDecisionBeyondLegacyBudget(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         strings.Repeat("decision-", 40),
		"evidence":         strings.Repeat("evidence-", 120),
		"options":          strings.Repeat("option-", 120),
		"recommendation":   strings.Repeat("recommendation-", 70),
		"test_obligations": strings.Repeat("test-", 120),
		"targets":          []string{"glm-worker/internal/parentactioncmd/terminal_projection.go:1-220"},
	})
	if len(terminal) > packet.MaxPacketBytes {
		t.Fatalf("fixture must remain a valid packet-sized semantic result: bytes=%d max=%d", len(terminal), packet.MaxPacketBytes)
	}
	handoff := representativeHandoff(t, strings.Repeat("baseline-", 180), strings.Repeat("validation-", 180))

	var stdout bytes.Buffer
	if err := writeProjectedTerminalEnvelope(&stdout, terminal, handoff); err != nil {
		t.Fatalf("write projected terminal envelope: %v", err)
	}
	if stdout.Len() > parentActionTerminalBudgetBytes {
		t.Fatalf("model-visible stdout exceeds budget: bytes=%d budget=%d", stdout.Len(), parentActionTerminalBudgetBytes)
	}
	machineJSON, err := decodeSingleMachineJSON(stdout.Bytes(), "projected terminal envelope")
	if err != nil {
		t.Fatalf("projected stdout is not one machine JSON value: %v", err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(machineJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	assertEnvelopeWithinBudget(t, envelope)
	if envelope.Projection == nil {
		t.Fatal("projection telemetry missing")
	}
	if envelope.Projection.ProjectedBytes <= 2400 {
		t.Fatalf("fixture must exercise the reopened >2400-byte regression: projected=%d", envelope.Projection.ProjectedBytes)
	}
	if envelope.Projection.RawBytes <= envelope.Projection.ProjectedBytes {
		t.Fatalf("projection did not reduce model-visible bytes: %+v", envelope.Projection)
	}
	if envelope.Projection.ProjectedBytes != stdout.Len() {
		t.Fatalf("projected_bytes=%d stdout=%d", envelope.Projection.ProjectedBytes, stdout.Len())
	}
	if envelope.Projection.BudgetBytes != parentActionTerminalBudgetBytes {
		t.Fatalf("budget=%d want=%d", envelope.Projection.BudgetBytes, parentActionTerminalBudgetBytes)
	}
	if envelope.Projection.Overflow || envelope.Projection.RecoveryCalls != 0 || envelope.Projection.ParentToolCalls != 1 {
		t.Fatalf("normal terminal unexpectedly overflowed or required recovery: %+v", envelope.Projection)
	}
	for _, field := range []string{"status", "risk", "decision", "evidence", "options", "recommendation", "test_obligations", "targets"} {
		assertJSONFieldEqual(t, terminal, envelope.Terminal, field)
	}
	assertJSONFieldAbsent(t, envelope.Terminal, "artifacts")
	assertHandoffAuthorityPreserved(t, envelope.Handoff)
}
