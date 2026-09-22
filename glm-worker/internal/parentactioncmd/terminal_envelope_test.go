package parentactioncmd

import (
	"bytes"
	"encoding/json"
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

func TestDecodeSingleMachineJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "single object", raw: `{"status":"ok"}`},
		{name: "single array", raw: `[1,2]`},
		{name: "empty", raw: `` , wantErr: true},
		{name: "null", raw: `null`, wantErr: true},
		{name: "multiple", raw: `{} {}`, wantErr: true},
		{name: "trailing junk", raw: `{} nope`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeSingleMachineJSON([]byte(tt.raw), "test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeSingleMachineJSON() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteProjectedTerminalEnvelope(t *testing.T) {
	t.Parallel()

	terminal := json.RawMessage(`{"status":"accepted"}`)
	handoff := json.RawMessage(`{"state":"active"}`)
	var out bytes.Buffer
	if err := writeProjectedTerminalEnvelope(&out, terminal, handoff); err != nil {
		t.Fatalf("writeProjectedTerminalEnvelope() error = %v", err)
	}
	var got parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got.Status != "parent_action_terminal" {
		t.Fatalf("Status = %q, want parent_action_terminal", got.Status)
	}
	if strings.TrimSpace(string(got.Terminal)) != string(terminal) {
		t.Fatalf("Terminal = %s, want %s", got.Terminal, terminal)
	}
	if strings.TrimSpace(string(got.Handoff)) != string(handoff) {
		t.Fatalf("Handoff = %s, want %s", got.Handoff, handoff)
	}
}
