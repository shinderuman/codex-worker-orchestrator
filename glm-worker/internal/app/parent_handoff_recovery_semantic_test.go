package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffRecoveryIncludesReviewerSemanticResult(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	result := recoveryReviewerDecisionResult()
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "haiku"}); err != nil {
		t.Fatal(err)
	}
	machine, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(machine) <= 2400 {
		t.Fatalf("fixture must exceed the parent-action terminal projection budget: bytes=%d", len(machine))
	}
	if err := st.Write(parentHandoffRecoverySemanticStateKey, string(machine)); err != nil {
		t.Fatal(err)
	}
	recordRecoverySemanticMaterial(t, st, state.ReviewerRole, "call-reviewer-decision")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SemanticResult struct {
			Availability string          `json:"availability"`
			Authority    string          `json:"authority"`
			StateKey     string          `json:"state_key"`
			CallID       string          `json:"call_id"`
			Packet       json.RawMessage `json:"packet"`
			Reason       string          `json:"reason"`
		} `json:"semantic_result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	if raw.SemanticResult.Availability != "available" || raw.SemanticResult.Authority != "task-state" ||
		raw.SemanticResult.StateKey != parentHandoffRecoverySemanticStateKey || raw.SemanticResult.CallID != "call-reviewer-decision" || raw.SemanticResult.Reason != "" {
		t.Fatalf("semantic result locator = %#v", raw.SemanticResult)
	}
	parsed, err := packet.ParseStructured(raw.SemanticResult.Packet)
	if err != nil {
		t.Fatalf("semantic packet parse: %v", err)
	}
	if parsed.Status != packet.StatusNeedsSolDecision || parsed.Decision != result.Decision || parsed.Evidence != result.Evidence || parsed.Options != result.Options {
		t.Fatalf("semantic packet = %#v", parsed)
	}
}

func TestParentHandoffRecoveryMarksMissingReviewerSemanticResultUnavailable(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	result := recoveryReviewerDecisionResult()
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "haiku"}); err != nil {
		t.Fatal(err)
	}
	recordRecoverySemanticMaterial(t, st, state.ReviewerRole, "call-reviewer-missing")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	semantic, ok := raw["semantic_result"].(map[string]any)
	if !ok || semantic["availability"] != "unavailable" || semantic["reason"] != "canonical-review-packet-missing" || semantic["call_id"] != "call-reviewer-missing" {
		t.Fatalf("missing semantic result = %#v", raw["semantic_result"])
	}
	if _, exists := semantic["packet"]; exists {
		t.Fatalf("missing semantic result unexpectedly contained packet: %#v", semantic)
	}
}

func TestParentHandoffRecoveryDoesNotAttachStaleReviewToWorkerDecision(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	result := recoveryReviewerDecisionResult()
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(result, state.ParentReviewProducer{Role: string(state.WorkerRole), Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	machine, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(parentHandoffRecoverySemanticStateKey, string(machine)); err != nil {
		t.Fatal(err)
	}
	recordRecoverySemanticMaterial(t, st, state.WorkerRole, "call-worker-decision")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	if _, exists := raw["semantic_result"]; exists {
		t.Fatalf("worker decision received stale reviewer semantic result: %s", stdout.String())
	}
}

func recoveryReviewerDecisionResult() packet.Result {
	return packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Decision:        "choose the bounded implementation path",
		Evidence:        strings.Repeat("reviewer semantic evidence remains exact; ", 32),
		Options:         strings.Repeat("retain current behavior or apply the bounded change; ", 28),
		Recommendation:  "apply the bounded change",
		TestObligations: "run focused recovery tests and the repository quality gate",
		Targets:         []string{"glm-worker/internal/app/parent_handoff_recovery_semantic.go"},
	}
}

func recordRecoverySemanticMaterial(t *testing.T, st *state.StateStore, role state.SessionRole, callID string) {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:       callID,
		CallType:     state.CallTypeTask,
		TaskID:       taskID,
		Phase:        "reviewer-1",
		Role:         role,
		ModelAlias:   "haiku",
		Outcome:      "success",
		PacketStatus: string(packet.StatusNeedsSolDecision),
	})
}
