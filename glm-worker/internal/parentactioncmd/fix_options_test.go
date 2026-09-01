package parentactioncmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestInvalidFixOptionsAreRejectedBeforeStagedPayloadConsumption(t *testing.T) {
	repoRoot := t.TempDir()
	prepared, err := parentaction.Prepare(repoRoot, string(parentaction.ActionFix))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := parentaction.LookupPayloadAction(string(parentaction.ActionFix))
	if !ok {
		t.Fatal("fix payload descriptor missing")
	}

	var output bytes.Buffer
	err = executePayloadAction(
		repoRoot,
		descriptor,
		[]string{prepared.Token, "--approval-only"},
		&output,
		&output,
		nil,
	)
	if err == nil {
		t.Fatal("invalid approval-only options were accepted")
	}
	if _, statErr := os.Stat(prepared.Path); statErr != nil {
		t.Fatalf("staged payload was consumed on option error: %v", statErr)
	}
}
