package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTerminalContinuationOverflowFallsBackToStructuredFailure(t *testing.T) {
	stats := parentActionTerminalProjectionStats{
		BudgetBytes:     1024,
		RawBytes:        4096,
		TerminalMode:    "identity-only",
		HandoffMode:     "overflow-minimal",
		Overflow:        true,
		ParentToolCalls: 1,
	}
	failure := parentActionTerminalEnvelopePayload{
		Status:          "parent_action_terminal_projection_overflow",
		Terminal:        json.RawMessage(`{"status":"FIX_REQUIRED"}`),
		ProjectionError: "projection overflow",
		Projection:      &stats,
	}
	if err := finalizeProjectionStats(&failure, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.ProjectedBytes > stats.BudgetBytes {
		t.Fatalf("plain failure fixture exceeds budget: %+v", stats)
	}

	continuation := parentActionTerminalContinuation{
		Kind:    "terminal-json-artifact",
		Locator: strings.Repeat("x", 2048),
		Bytes:   100,
		SHA256:  strings.Repeat("a", 64),
	}
	var stdout bytes.Buffer
	rootErr := errors.New("projection overflow")
	if err := writeTerminalProjectionContinuation(&stdout, failure, continuation, rootErr); !errors.Is(err, rootErr) {
		t.Fatalf("error = %v want root projection error", err)
	}
	machineJSON, err := decodeSingleMachineJSON(stdout.Bytes(), "terminal continuation fallback")
	if err != nil {
		t.Fatalf("structured fallback missing: %v", err)
	}
	var got parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(machineJSON, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != failure.Status || got.Continuation != nil {
		t.Fatalf("fallback = %+v", got)
	}
	if stdout.Len() > stats.BudgetBytes {
		t.Fatalf("fallback exceeds budget: bytes=%d budget=%d", stdout.Len(), stats.BudgetBytes)
	}
}
