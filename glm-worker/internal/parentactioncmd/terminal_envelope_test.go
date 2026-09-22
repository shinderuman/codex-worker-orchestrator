package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParentActionCommandMetadataPreservesTerminalEnvelopeMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action      string
		terminal    bool
		inProcess   bool
		wantPresent bool
	}{
		{action: "start", terminal: true, wantPresent: true},
		{action: "approve-surface", terminal: true, wantPresent: true},
		{action: "accept", terminal: true, wantPresent: true},
		{action: "resume", terminal: true, wantPresent: true},
		{action: "no-go", terminal: true, wantPresent: true},
		{action: actionRecordPublicationFinding, terminal: true, wantPresent: true},
		{action: actionRecordDefectFinding, terminal: true, wantPresent: true},
		{action: actionBindDefectTask, terminal: true, wantPresent: true},
		{action: actionImprovementDisposition, terminal: true, wantPresent: true},
		{action: "reopen", terminal: true, wantPresent: true},
		{action: "park", terminal: true, wantPresent: true},
		{action: "unpark", terminal: true, wantPresent: true},
		{action: actionReviewEvidence, terminal: true, inProcess: true, wantPresent: true},
		{action: "decision", terminal: true, wantPresent: true},
		{action: "fix", terminal: true, wantPresent: true},
		{action: "start-milestones", terminal: true, wantPresent: true},
		{action: "revise-milestones", terminal: false, wantPresent: true},
		{action: "rotation-claim", terminal: false, wantPresent: true},
		{action: "rotation-bind", terminal: false, wantPresent: true},
		{action: "rotation-fail", terminal: false, wantPresent: true},
		{action: "complete", terminal: false, wantPresent: true},
		{action: "install", terminal: false, wantPresent: true},
		{action: "wait", terminal: false, wantPresent: true},
		{action: actionContinuationStopHook, terminal: false, wantPresent: true},
		{action: actionContinuationMetadataGuard, terminal: false, wantPresent: true},
		{action: "evidence", terminal: false, wantPresent: true},
		{action: "finalize-check", terminal: false, wantPresent: true},
		{action: "push-binding", terminal: false, wantPresent: true},
		{action: "definitely-unknown", terminal: false, wantPresent: false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			descriptor, ok := lookupParentActionCommand(tt.action)
			if ok != tt.wantPresent {
				t.Fatalf("lookupParentActionCommand(%q) present=%v want %v", tt.action, ok, tt.wantPresent)
			}
			if !ok {
				return
			}
			if descriptor.TerminalEnvelope != tt.terminal {
				t.Fatalf("lookupParentActionCommand(%q).TerminalEnvelope=%v want %v", tt.action, descriptor.TerminalEnvelope, tt.terminal)
			}
			if descriptor.InProcessHandoff != tt.inProcess {
				t.Fatalf("lookupParentActionCommand(%q).InProcessHandoff=%v want %v", tt.action, descriptor.InProcessHandoff, tt.inProcess)
			}
		})
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
