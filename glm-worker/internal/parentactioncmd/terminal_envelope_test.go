package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestParentActionCommandMetadataPreservesTerminalEnvelopeMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action      string
		terminal    bool
		wantPresent bool
	}{
		{action: "start", terminal: true, wantPresent: true},
		{action: "approve-surface", terminal: true, wantPresent: true},
		{action: "accept", terminal: true, wantPresent: true},
		{action: "resume", terminal: true, wantPresent: true},
		{action: "decision", terminal: true, wantPresent: true},
		{action: "fix", terminal: true, wantPresent: true},
		{action: "start-milestones", terminal: true, wantPresent: true},
		{action: "revise-milestones", terminal: false, wantPresent: true},
		{action: "rotation-claim", terminal: false, wantPresent: true},
		{action: "rotation-bind", terminal: false, wantPresent: true},
		{action: "rotation-fail", terminal: false, wantPresent: true},
		{action: "wait", terminal: false, wantPresent: true},
		{action: actionContinuationStopHook, terminal: false, wantPresent: true},
		{action: actionContinuationMetadataGuard, terminal: false, wantPresent: true},
		{action: "evidence", terminal: false, wantPresent: true},
		{action: "finalize-check", terminal: false, wantPresent: true},
		{action: "resync-source", terminal: false, wantPresent: true},
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
	err := writeTerminalHandoffFailure(config.AppConfig{}, &stdout, json.RawMessage(`{"status":"PASS"}`), nil, handoffErr)
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

func TestReadTerminalHandoffSelectsBoundedWorkerView(t *testing.T) {
	root := t.TempDir()
	worker := filepath.Join(root, "glm-worker")
	script := `#!/bin/sh
set -eu
test "$#" -eq 2 && test "$1" = "--handoff"
case "$2" in
  terminal) printf '%s\n' '{"consistent":true,"required_action":"accept"}' ;;
  recovery) printf '%s\n' '{"consistent":false,"projection":"recovery"}' ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(worker, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.AppConfig{RepoRoot: root}
	for _, tc := range []struct {
		recovery bool
		field    string
	}{
		{false, `"required_action":"accept"`},
		{true, `"projection":"recovery"`},
	} {
		var stderr bytes.Buffer
		raw, err := readTerminalHandoff(cfg, &stderr, tc.recovery)
		if err != nil || !bytes.Contains(raw, []byte(tc.field)) {
			t.Fatalf("recovery=%v: handoff=%s, error=%v, stderr=%s", tc.recovery, raw, err, &stderr)
		}
	}
}
