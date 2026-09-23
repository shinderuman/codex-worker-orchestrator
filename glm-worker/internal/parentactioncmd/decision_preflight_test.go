package parentactioncmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestDecisionPreflightRejectsBeforeConsumingStagedPayload(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	payload := []byte("EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\n")
	prepared, err := parentaction.Prepare(cfg.RepoRoot, string(parentaction.ActionDecision))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline < 0 {
		t.Fatal("prepared payload has no token header newline")
	}
	staged := append(append([]byte(nil), raw[:newline+1]...), payload...)
	if err := os.WriteFile(prepared.Path, staged, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = writeParentActionWorkerStub(t, cfg, false)

	var stdout, stderr bytes.Buffer
	if err := execute(cfg, []string{string(parentaction.ActionDecision), prepared.Token}, &stdout, &stderr); err == nil {
		t.Fatal("invalid staged decision was accepted")
	}
	after, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("rejected staged decision was consumed: %v", err)
	}
	if !bytes.Equal(after, staged) {
		t.Fatalf("rejected staged decision changed: got %q want %q", after, staged)
	}
}
