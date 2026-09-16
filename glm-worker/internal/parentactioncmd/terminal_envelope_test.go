package parentactioncmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTerminalEnvelopeActionCoversParentLifecycleActions(t *testing.T) {
	for _, action := range []string{"start", "decision", "fix", "start-milestones", "approve-surface", "accept", "resume", "no-go", "park", "unpark"} {
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
