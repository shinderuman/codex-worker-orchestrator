package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTerminalOverflowPersistsExactBoundedContinuation(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         strings.Repeat("d", 1200),
		"evidence":         strings.Repeat("e", 1200),
		"options":          strings.Repeat("o", 1200),
		"recommendation":   strings.Repeat("r", 900),
		"test_obligations": strings.Repeat("t", 900),
		"targets":          []string{"glm-worker/internal/parentactioncmd/terminal_projection.go:1-220"},
	})
	if len(terminal) >= packet.MaxPacketBytes {
		t.Fatalf("fixture must remain a valid packet-sized semantic result: bytes=%d max=%d", len(terminal), packet.MaxPacketBytes)
	}
	if len(terminal) < 5*1024 {
		t.Fatalf("fixture must exercise a near-budget semantic result: bytes=%d", len(terminal))
	}
	handoff := mustJSONRaw(t, map[string]any{
		"version":                    3,
		"consistent":                 true,
		"inconsistency":              nil,
		"task_id":                    "task-1",
		"task_status":                "waiting-decision",
		"required_action":            "decision",
		"allowed_actions":            []string{"decision", "park"},
		"required_action_parameters": map[string]string{},
		"resume_kind":                "decision",
		"pending_decision":           true,
		"parent_review_open":         nil,
		"artifact_dir":               "/tmp/task",
		"last_material": map[string]any{
			"call_id":       "call-1",
			"call_type":     "reviewer",
			"phase":         "review",
			"outcome":       "terminal",
			"packet_status": "NEEDS_SOL_DECISION",
		},
		"session_rotation": map[string]any{"state": "not_required"},
		"parent_request": map[string]any{
			"completion_admitted": false,
			"stop_admitted":       false,
			"continuation":        strings.Repeat("parent-request-detail-", 180),
		},
		"action_specs": map[string]any{
			"decision": map[string]any{"kind": "staged", "prepare_command": []string{"glm-parent-action", "prepare", "decision"}},
			"park":     map[string]any{"kind": "direct", "command": []string{"glm-parent-action", "park"}},
		},
	})
	if _, err := projectParentActionTerminalEnvelope(terminal, handoff); err == nil {
		t.Fatal("fixture must overflow the combined terminal projection budget")
	}

	cfg := config.AppConfig{
		RepoRoot:  t.TempDir(),
		StateBase: t.TempDir(),
		RepoHash:  "terminal-continuation-test",
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", "task-1"); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	projectionErr := writeProjectedTerminalEnvelopeWithContinuation(cfg, &stdout, terminal, handoff)
	if projectionErr == nil {
		t.Fatal("overflow must remain fail-closed")
	}
	if stdout.Len() > parentActionTerminalBudgetBytes {
		t.Fatalf("model-visible overflow continuation exceeds budget: bytes=%d budget=%d", stdout.Len(), parentActionTerminalBudgetBytes)
	}
	machineJSON, err := decodeSingleMachineJSON(stdout.Bytes(), "overflow continuation envelope")
	if err != nil {
		t.Fatal(err)
	}
	var envelope parentActionTerminalContinuationEnvelope
	if err := json.Unmarshal(machineJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "parent_action_terminal_projection_overflow" {
		t.Fatalf("status=%q", envelope.Status)
	}
	if envelope.Projection == nil || !envelope.Projection.Overflow || envelope.Projection.RecoveryCalls != 0 {
		t.Fatalf("unexpected projection telemetry: %+v", envelope.Projection)
	}
	if envelope.Projection.ProjectedBytes != stdout.Len() {
		t.Fatalf("projected_bytes=%d stdout=%d", envelope.Projection.ProjectedBytes, stdout.Len())
	}
	if envelope.Continuation.Kind != "terminal-json-artifact" {
		t.Fatalf("continuation kind=%q", envelope.Continuation.Kind)
	}
	if envelope.Continuation.Bytes != len(terminal) {
		t.Fatalf("continuation bytes=%d terminal=%d", envelope.Continuation.Bytes, len(terminal))
	}
	persisted, err := os.ReadFile(envelope.Continuation.Locator)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted, terminal) {
		t.Fatalf("continuation does not preserve exact semantic terminal")
	}
	if !strings.HasPrefix(envelope.Continuation.Locator, st.ArtifactDir("task-1")+string(os.PathSeparator)) {
		t.Fatalf("continuation locator escaped task artifact dir: %s", envelope.Continuation.Locator)
	}
	if envelope.Continuation.SHA256 == "" {
		t.Fatal("continuation sha256 missing")
	}
	assertHandoffRequiredActionSpecPreserved(t, envelope.Handoff)
}
