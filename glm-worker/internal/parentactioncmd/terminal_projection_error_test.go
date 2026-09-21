package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWriteFailedTerminalActionProjectsInterruptedErrorWithRecoveryHandoff(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"error": map[string]any{
			"kind":    "interrupted",
			"message": "task interrupted by glm-worker --stop; task is stopped and resumable",
			"detail": map[string]any{
				"phase":            "review",
				"task_id":          "12345678-1111-2222-3333-444444444444",
				"repo_root":        "/tmp/repo",
				"resume_available": true,
			},
		},
	})
	handoff := representativeHandoff(t, "small", "small")
	terminalErr := errors.New("interrupted child exit")
	var stdout bytes.Buffer

	err := writeFailedTerminalAction(&stdout, append(append([]byte(nil), terminal...), '\n'), terminalErr, func() (json.RawMessage, error) {
		return handoff, nil
	})
	if !errors.Is(err, terminalErr) {
		t.Fatalf("error = %v, want original terminal error", err)
	}
	if stdout.Len() > parentActionTerminalBudgetBytes {
		t.Fatalf("model-visible stdout exceeds budget: bytes=%d budget=%d", stdout.Len(), parentActionTerminalBudgetBytes)
	}
	machineJSON, err := decodeSingleMachineJSON(stdout.Bytes(), "failed terminal projection")
	if err != nil {
		t.Fatal(err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(machineJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Projection == nil || envelope.Projection.RecoveryCalls != 1 || envelope.Projection.ParentToolCalls != 1 {
		t.Fatalf("recovery telemetry = %+v", envelope.Projection)
	}
	if envelope.Projection.HandoffMode != "recovery-bounded" {
		t.Fatalf("handoff mode = %q", envelope.Projection.HandoffMode)
	}
	assertProcessErrorIdentity(t, envelope.Terminal, "interrupted", "task interrupted by glm-worker --stop; task is stopped and resumable")
	assertHandoffAuthorityPreserved(t, envelope.Handoff)
}

func TestWriteFailedTerminalActionFailsClosedForOversizedWorkerError(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"error": map[string]any{
			"kind":    "worker_error",
			"message": "worker exited before a valid packet was produced",
			"detail": map[string]any{
				"phase":       "worker",
				"exit_code":   1,
				"output_tail": strings.Repeat("large-worker-output-", 500),
			},
		},
	})
	handoff := representativeHandoff(t, "small", "small")
	terminalErr := errors.New("worker child exit")
	var stdout bytes.Buffer

	err := writeFailedTerminalAction(&stdout, append(append([]byte(nil), terminal...), '\n'), terminalErr, func() (json.RawMessage, error) {
		return handoff, nil
	})
	if !errors.Is(err, terminalErr) {
		t.Fatalf("error = %v, want original terminal error", err)
	}
	if stdout.Len() > parentActionTerminalBudgetBytes {
		t.Fatalf("model-visible stdout exceeds budget: bytes=%d budget=%d", stdout.Len(), parentActionTerminalBudgetBytes)
	}
	machineJSON, err := decodeSingleMachineJSON(stdout.Bytes(), "worker error overflow projection")
	if err != nil {
		t.Fatal(err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(machineJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "parent_action_terminal_projection_overflow" {
		t.Fatalf("status = %q", envelope.Status)
	}
	if envelope.Projection == nil || !envelope.Projection.Overflow || envelope.Projection.RecoveryCalls != 1 {
		t.Fatalf("overflow recovery telemetry = %+v", envelope.Projection)
	}
	assertProcessErrorIdentity(t, envelope.Terminal, "worker_error", "worker exited before a valid packet was produced")
	var projected map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Terminal, &projected); err != nil {
		t.Fatal(err)
	}
	var processError map[string]json.RawMessage
	if err := json.Unmarshal(projected["error"], &processError); err != nil {
		t.Fatal(err)
	}
	if _, ok := processError["detail"]; ok {
		t.Fatalf("oversized diagnostic detail leaked into overflow identity: %s", envelope.Terminal)
	}
	assertHandoffRequiredActionSpecPreserved(t, envelope.Handoff)
}

func assertProcessErrorIdentity(t *testing.T, raw json.RawMessage, kind, message string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	var processError struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(object["error"], &processError); err != nil {
		t.Fatal(err)
	}
	if processError.Kind != kind || processError.Message != message {
		t.Fatalf("process error identity = %+v", processError)
	}
}
