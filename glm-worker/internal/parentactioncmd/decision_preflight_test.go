package parentactioncmd

import (
	"bytes"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestDecisionPreflightRejectsBeforeConsumingStagedPayload(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	payload := []byte("EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\n")
	token := prepareParentPayload(t, cfg, string(parentaction.ActionDecision), payload)
	_ = writeParentActionWorkerStub(t, cfg, false)

	var stdout, stderr bytes.Buffer
	if err := execute(cfg, []string{string(parentaction.ActionDecision), token}, &stdout, &stderr); err == nil {
		t.Fatal("invalid staged decision was accepted")
	}
	peeked, err := parentaction.Peek(cfg.RepoRoot, string(parentaction.ActionDecision), token)
	if err != nil {
		t.Fatalf("rejected staged decision was consumed: %v", err)
	}
	if !bytes.Equal(peeked, payload) {
		t.Fatalf("rejected staged decision changed: got %q want %q", peeked, payload)
	}
}
