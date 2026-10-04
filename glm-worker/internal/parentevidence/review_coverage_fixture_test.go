package parentevidence

import (
	"bytes"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func projectReviewCoverageManifest(t *testing.T, repoRoot string, st *state.StateStore, manifest Manifest) {
	t.Helper()
	ownerCallID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, ownerCallID, Providers{})
	projector.Project(manifest)
	if err := projector.Commit(&bytes.Buffer{}, manifest.Reason); err != nil {
		t.Fatal(err)
	}
}
