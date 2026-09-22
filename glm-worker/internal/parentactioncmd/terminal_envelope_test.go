package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParentActionEnvelopeMatrix(t *testing.T) {
	tests := []struct {
		action          string
		terminal        bool
		inProcessHandoff bool
	}{
		{action: "start", terminal: true},
		{action: "decision", terminal: true},
		{action: "fix", terminal: true},
		{action: "start-milestones", terminal: true},
		{action: "revise-milestones", terminal: false},
		{action: "approve-surface", terminal: true},
		{action: "accept", terminal: true},
		{action: "resume", terminal: true},
		{action: "no-go", terminal: true},
		{action: actionRecordPublicationFinding, terminal: true},
		{action: actionRecordDefectFinding, terminal: true},
		{action: actionBindDefectTask, terminal: true},
		{action: actionImprovementDisposition, terminal: true},
		{action: "reopen", terminal: true},
		{action: "park", terminal: true},
		{action: "unpark", terminal: true},
		{action: "review-evidence", terminal: true, inProcessHandoff: true},
		{action: "rotation-claim", terminal: false},
		{action: "rotation-bind", terminal: false},
		{action: "rotation-fail", terminal: false},
		{action: "complete", terminal: false},
		{action: "install", terminal: false},
		{action: "wait", terminal: false},
		{action: "evidence", terminal: false},
		{action: "finalize-check", terminal: false},
		{action: "push-binding", terminal: false},
		{action: actionContinuationStopHook, terminal: false},
		{action: actionContinuationMetadataGuard, terminal: false},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			if got := terminalEnvelopeAction(tt.action); got != tt.terminal {
				t.Fatalf("terminalEnvelopeAction(%q) = %v, want %v", tt.action, got, tt.terminal)
			}
			if got := parentActionUsesInProcessHandoff(tt.action); got != tt.inProcessHandoff {
				t.Fatalf("parentActionUsesInProcessHandoff(%q) = %v, want %v", tt.action, got, tt.inProcessHandoff)
			}
		})
	}
	if terminalEnvelopeAction("unknown-action") {
		t.Fatal("unknown action must not participate in terminal envelope")
	}
}

func TestDecodeSingleMachineJSONRejectsAmbiguousOutput(t *testing.T) {
	if _, err := decodeSingleMachineJSON([]byte(`{"ok":true}`), "result"); err != nil {
		t.Fatalf("single JSON rejected: %v", err)
	}
	for _, raw := range []string{"", "null", `{"a":1}\n{"b":2}`, `{"a":1} trailing`} {
		if _, err := decodeSingleMachineJSON([]byte(raw), "result"); err == nil {
			t.Fatalf("ambiguous machine output accepted: %q", raw)
		}
	}
}

func TestExecuteWithTerminalEnvelope(t *testing.T) {
	envelope := parentActionTerminalEnvelope(
		json.RawMessage(`{"status":"PASS"}`),
		json.RawMessage(`{"consistent":true,"required_action":"accept"}`),
	)
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"status":"parent_action_terminal"`, `"terminal":{"status":"PASS"}`, `"handoff":{"consistent":true,"required_action":"accept"}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("terminal envelope missing %s: %s", want, text)
		}
	}
}

func TestWriteTerminalHandoffFailurePreservesTerminalResult(t *testing.T) {
	var stdout bytes.Buffer
	handoffErr := errors.New("handoff unavailable")
	err := writeTerminalHandoffFailure(&stdout, json.RawMessage(`{"status":"PASS"}`), handoffErr)
	if !errors.Is(err, handoffErr) {
		t.Fatalf("error = %v, want %v", err, handoffErr)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "parent_action_terminal_handoff_failed" {
		t.Fatalf("status = %q", envelope.Status)
	}
	if string(envelope.Terminal) != `{"status":"PASS"}` {
		t.Fatalf("terminal = %s", envelope.Terminal)
	}
	if envelope.HandoffError != handoffErr.Error() {
		t.Fatalf("handoff error = %q", envelope.HandoffError)
	}
	if len(envelope.Handoff) != 0 {
		t.Fatalf("handoff must be absent on failure: %s", envelope.Handoff)
	}
}
