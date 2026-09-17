package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTerminalEnvelopeActionCoversParentLifecycleActions(t *testing.T) {
	for _, action := range []string{"start", "decision", "fix", "start-milestones", "approve-surface", "accept", "resume", "no-go", "park", "unpark", "review-evidence"} {
		if !terminalEnvelopeAction(action) {
			t.Fatalf("action %q must return a machine terminal envelope", action)
		}
	}
	for _, action := range []string{"prepare", "revise-milestones", "complete", "install", "wait", "evidence", "finalize-check", "push-binding"} {
		if terminalEnvelopeAction(action) {
			t.Fatalf("action %q already owns a different output contract", action)
		}
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
