package parentactioncmd

import (
	"io"

	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestReviewEvidenceParentActionRejectsArguments(t *testing.T) {
	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "parent-review-evidence-action", RepoRoot: t.TempDir()}
	if err := executeWithTerminalEnvelope(cfg, []string{actionReviewEvidence, "extra"}, io.Discard, io.Discard); err == nil {
		t.Fatal("review-evidence accepted unexpected arguments")
	}
}
