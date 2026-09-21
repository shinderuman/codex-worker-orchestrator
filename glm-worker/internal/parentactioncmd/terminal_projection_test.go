package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTerminalProjectionBoundsDecisionAndPreservesSemanticFields(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         "choose the safe protocol boundary",
		"evidence":         "the worker found a semantic ambiguity at the public boundary",
		"options":          "keep internal-only or expose the field",
		"recommendation":   "keep the field internal until Sol approves exposure",
		"test_obligations": "cover both internal and exposed protocol branches",
		"targets":          []string{"glm-worker/internal/packet/result.go:40-70"},
		"artifacts":        []string{"/tmp/task/decision.json"},
	})
	handoff := representativeHandoff(t, strings.Repeat("baseline-detail-", 220), strings.Repeat("validation-detail-", 220))

	rawEnvelope, err := json.Marshal(parentActionTerminalEnvelope(terminal, handoff))
	if err != nil {
		t.Fatal(err)
	}
	if encodedJSONLineBytes(rawEnvelope) < 16000 {
		t.Fatalf("fixture must stay at least as large as the observed truncating result: raw=%d", encodedJSONLineBytes(rawEnvelope))
	}

	envelope, err := projectParentActionTerminalEnvelope(terminal, handoff)
	if err != nil {
		t.Fatalf("projection failed: %v", err)
	}
	assertEnvelopeWithinBudget(t, envelope)
	if envelope.Projection == nil {
		t.Fatal("projection telemetry missing")
	}
	if envelope.Projection.RawBytes <= envelope.Projection.ProjectedBytes {
		t.Fatalf("projection did not reduce bytes: %+v", envelope.Projection)
	}
	if envelope.Projection.ParentToolCalls != 1 || envelope.Projection.RecoveryCalls != 0 {
		t.Fatalf("unexpected call telemetry: %+v", envelope.Projection)
	}
	if envelope.Projection.DeduplicatedFields == 0 {
		t.Fatalf("dedup telemetry missing: %+v", envelope.Projection)
	}
	for _, field := range []string{"status", "risk", "decision", "evidence", "options", "recommendation", "test_obligations", "targets", "artifacts"} {
		assertJSONFieldEqual(t, terminal, envelope.Terminal, field)
	}
	assertHandoffAuthorityPreserved(t, envelope.Handoff)
	assertJSONFieldAbsent(t, envelope.Handoff, "baseline")
	assertJSONFieldAbsent(t, envelope.Handoff, "snapshot")
	assertJSONFieldAbsent(t, envelope.Handoff, "validations")
	assertJSONFieldAbsent(t, envelope.Handoff, "routing_evidence")
}

func TestWriteProjectedTerminalEnvelopeReturnsSingleBoundedResultWithoutRecovery(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         "choose the safe protocol boundary",
		"evidence":         "bounded evidence",
		"options":          "safe or unsafe",
		"recommendation":   "safe",
		"test_obligations": "preserve the canonical next action",
		"targets":          []string{"target.go:10-20"},
		"artifacts":        []string{"/tmp/task/decision.json"},
	})
	handoff := representativeHandoff(t, strings.Repeat("baseline-", 300), strings.Repeat("validation-", 300))

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
	if envelope.Projection == nil || envelope.Projection.ParentToolCalls != 1 || envelope.Projection.RecoveryCalls != 0 {
		t.Fatalf("normal terminal unexpectedly requires recovery: %+v", envelope.Projection)
	}
	if envelope.Projection.ProjectedBytes != stdout.Len() {
		t.Fatalf("projected_bytes=%d stdout=%d", envelope.Projection.ProjectedBytes, stdout.Len())
	}
	assertJSONFieldEqual(t, terminal, envelope.Terminal, "decision")
	assertHandoffAuthorityPreserved(t, envelope.Handoff)
}

func TestTerminalProjectionPreservesNeedsSolReviewAndPassFields(t *testing.T) {
	cases := []struct {
		name   string
		status string
		fields map[string]any
		keep   []string
	}{
		{
			name:   "needs sol review",
			status: "NEEDS_SOL_REVIEW",
			fields: map[string]any{
				"summary":              "review summary",
				"requirement_coverage": "all requirements covered",
				"invariants":           "machine authority remains unchanged",
				"test_evidence":        "go test ./... passed",
				"issues":               "semantic exposure needs Sol judgment",
				"residual_risk":        "public compatibility",
				"sol_question":         "should this field be externally visible?",
			},
			keep: []string{"summary", "requirement_coverage", "invariants", "test_evidence", "issues", "residual_risk", "sol_question"},
		},
		{
			name:   "pass",
			status: "PASS",
			fields: map[string]any{
				"summary":              "review passed",
				"requirement_coverage": "complete",
				"invariants":           "preserved",
				"test_evidence":        "tests passed",
				"issues":               "none",
				"residual_risk":        "low",
			},
			keep: []string{"summary", "requirement_coverage", "invariants", "test_evidence", "issues", "residual_risk"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			object := map[string]any{"status": tc.status, "risk": map[bool]string{true: "HIGH", false: "LOW"}[tc.status == "NEEDS_SOL_REVIEW"], "targets": []string{"target.go:10-20"}, "artifacts": []string{"/tmp/task/review.json"}}
			for key, value := range tc.fields {
				object[key] = value
			}
			terminal := mustJSONRaw(t, object)
			handoff := representativeHandoff(t, strings.Repeat("baseline-", 260), strings.Repeat("validation-", 260))
			envelope, err := projectParentActionTerminalEnvelope(terminal, handoff)
			if err != nil {
				t.Fatalf("projection failed: %v", err)
			}
			assertEnvelopeWithinBudget(t, envelope)
			for _, field := range append([]string{"status", "risk", "targets", "artifacts"}, tc.keep...) {
				assertJSONFieldEqual(t, terminal, envelope.Terminal, field)
			}
			assertHandoffAuthorityPreserved(t, envelope.Handoff)
		})
	}
}

func TestTerminalProjectionLeavesSmallUnknownTerminalStatusesLossless(t *testing.T) {
	for _, status := range []string{"worker_error", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			terminal := mustJSONRaw(t, map[string]any{
				"status": status,
				"risk":   "HIGH",
				"error":  "provider stopped before a semantic packet was produced",
				"retry":  false,
			})
			handoff := representativeHandoff(t, "small", "small")
			envelope, err := projectParentActionTerminalEnvelope(terminal, handoff)
			if err != nil {
				t.Fatalf("projection failed: %v", err)
			}
			assertEnvelopeWithinBudget(t, envelope)
			if string(envelope.Terminal) != string(terminal) {
				t.Fatalf("unknown terminal status was changed:\nwant %s\n got %s", terminal, envelope.Terminal)
			}
			if envelope.Projection == nil || envelope.Projection.TerminalMode != "full" {
				t.Fatalf("terminal mode = %+v", envelope.Projection)
			}
			assertHandoffAuthorityPreserved(t, envelope.Handoff)
		})
	}
}

func TestTerminalProjectionReplacesOversizedRecoverableEvidenceWithArtifactLocators(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         "choose bounded transport",
		"evidence":         strings.Repeat("raw-evidence-that-is-recoverable-", 180),
		"options":          "bounded projection or raw envelope",
		"recommendation":   "bounded projection",
		"test_obligations": "verify semantic fields and exact artifact locators",
		"targets":          []string{"glm-worker/internal/parentactioncmd/terminal_projection.go:1-200"},
		"artifacts":        []string{"/tmp/task/evidence.json", "/tmp/task/terminal.json"},
	})
	handoff := representativeHandoff(t, strings.Repeat("baseline-", 180), strings.Repeat("validation-", 180))

	envelope, err := projectParentActionTerminalEnvelope(terminal, handoff)
	if err != nil {
		t.Fatalf("projection failed: %v", err)
	}
	assertEnvelopeWithinBudget(t, envelope)
	if envelope.Projection == nil || envelope.Projection.TerminalMode != "semantic-locators" {
		t.Fatalf("terminal mode = %+v", envelope.Projection)
	}
	var projected map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Terminal, &projected); err != nil {
		t.Fatal(err)
	}
	var evidence string
	if err := json.Unmarshal(projected["evidence"], &evidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(evidence, "raw-evidence-that-is-recoverable") {
		t.Fatalf("raw evidence leaked into bounded projection: %q", evidence)
	}
	if !strings.Contains(evidence, "artifact_count=2") || !strings.Contains(evidence, "exact locators") {
		t.Fatalf("evidence projection lacks locator contract: %q", evidence)
	}
	for _, field := range []string{"decision", "options", "recommendation", "test_obligations", "targets", "artifacts"} {
		assertJSONFieldEqual(t, terminal, envelope.Terminal, field)
	}
	if !containsString(envelope.Projection.ProjectedFields, "terminal.evidence") {
		t.Fatalf("projected field telemetry missing: %+v", envelope.Projection)
	}
}

func TestTerminalProjectionFailsClosedWhenMandatorySemanticFieldsCannotFit(t *testing.T) {
	terminal := mustJSONRaw(t, map[string]any{
		"status":           "NEEDS_SOL_DECISION",
		"risk":             "HIGH",
		"decision":         "a",
		"evidence":         "short evidence",
		"options":          strings.Repeat("mandatory-option-", 300),
		"recommendation":   strings.Repeat("mandatory-recommendation-", 160),
		"test_obligations": "must remain available to Sol",
		"targets":          []string{"target.go:1-2"},
	})
	handoff := representativeHandoff(t, "small", "small")

	_, err := projectParentActionTerminalEnvelope(terminal, handoff)
	var projectionErr *parentActionTerminalProjectionError
	if !errors.As(err, &projectionErr) {
		t.Fatalf("error = %v, want projection overflow", err)
	}
	failure, failureErr := writeTerminalProjectionFailurePayload(terminal, handoff, projectionErr)
	if failureErr != nil {
		t.Fatalf("build overflow payload: %v", failureErr)
	}
	assertEnvelopeWithinBudget(t, failure)
	if failure.Status != "parent_action_terminal_projection_overflow" {
		t.Fatalf("status = %q", failure.Status)
	}
	if failure.Projection == nil || !failure.Projection.Overflow {
		t.Fatalf("overflow telemetry missing: %+v", failure.Projection)
	}
	if failure.ProjectionError == "" {
		t.Fatal("projection_error missing")
	}
	assertJSONFieldEqual(t, terminal, failure.Terminal, "status")
	assertJSONFieldEqual(t, terminal, failure.Terminal, "risk")
	assertJSONFieldAbsent(t, failure.Terminal, "options")
	assertHandoffRequiredActionSpecPreserved(t, failure.Handoff)
}

func representativeHandoff(t *testing.T, baselineDetail, validationDetail string) json.RawMessage {
	t.Helper()
	return mustJSONRaw(t, map[string]any{
		"version":                    3,
		"consistent":                 true,
		"inconsistency":              nil,
		"task_id":                    "12345678-1111-2222-3333-444444444444",
		"task_status":                "waiting-sol-review",
		"required_action":            "decision",
		"allowed_actions":            []string{"decision", "park"},
		"required_action_parameters": map[string]string{},
		"resume_kind":                "review",
		"pending_decision":           true,
		"parent_review_open":         "review-1",
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
			"continuation":        map[string]any{"required": true, "reason": "waiting-sol-review"},
			"task_attribution":    map[string]any{"authority_task": "IMPLEMENTATION_TASKS/example.md", "handover": false},
		},
		"action_specs": map[string]any{
			"decision": map[string]any{"kind": "staged", "prepare_command": []string{"glm-parent-action", "prepare", "decision"}},
			"park":     map[string]any{"kind": "direct", "command": []string{"glm-parent-action", "park"}},
		},
		"baseline":         map[string]any{"head": baselineDetail, "index_digest": baselineDetail},
		"snapshot":         map[string]any{"head": baselineDetail, "worktree_digest": baselineDetail},
		"validations":      []map[string]any{{"validation_run_id": "run-1", "log": validationDetail, "working_dir": validationDetail}},
		"routing_evidence": []map[string]any{{"validation_run_id": "run-1", "working_dir": validationDetail, "snapshot_match": "exact"}},
		"publication":      map[string]any{"detail": validationDetail},
	})
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertEnvelopeWithinBudget(t *testing.T, envelope parentActionTerminalEnvelopePayload) {
	t.Helper()
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	actualBytes := encodedJSONLineBytes(raw)
	if actualBytes > parentActionTerminalBudgetBytes {
		t.Fatalf("envelope exceeds budget: size=%d budget=%d\n%s", actualBytes, parentActionTerminalBudgetBytes, raw)
	}
	if envelope.Projection != nil && envelope.Projection.ProjectedBytes != actualBytes {
		t.Fatalf("projected_bytes=%d actual=%d", envelope.Projection.ProjectedBytes, actualBytes)
	}
}

func assertJSONFieldEqual(t *testing.T, wantJSON, gotJSON json.RawMessage, field string) {
	t.Helper()
	var want, got map[string]json.RawMessage
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatal(err)
	}
	wantValue, wantOK := want[field]
	gotValue, gotOK := got[field]
	if wantOK != gotOK || string(wantValue) != string(gotValue) {
		t.Fatalf("field %q differs:\nwant %s\n got %s", field, wantValue, gotValue)
	}
}

func assertJSONFieldAbsent(t *testing.T, raw json.RawMessage, field string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object[field]; ok {
		t.Fatalf("field %q must be absent: %s", field, raw)
	}
}

func assertHandoffAuthorityPreserved(t *testing.T, raw json.RawMessage) {
	t.Helper()
	for _, field := range []string{"consistent", "task_id", "task_status", "required_action", "allowed_actions", "parent_request", "action_specs"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		if _, ok := object[field]; !ok {
			t.Fatalf("handoff authority field %q missing: %s", field, raw)
		}
	}
	assertHandoffRequiredActionSpecPreserved(t, raw)
}

func assertHandoffRequiredActionSpecPreserved(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	var required string
	if err := json.Unmarshal(object["required_action"], &required); err != nil {
		t.Fatal(err)
	}
	var specs map[string]json.RawMessage
	if err := json.Unmarshal(object["action_specs"], &specs); err != nil {
		t.Fatal(err)
	}
	if _, ok := specs[required]; !ok {
		t.Fatalf("required action %q lacks action spec: %s", required, raw)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
