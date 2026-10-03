package parentevidence

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestReviewTargetSourceProofBoundMatchesProjector(t *testing.T) {
	if MaxSourceLines != reviewtarget.MaxSourceProofLines {
		t.Fatalf("review target source proof bound = %d, projector bound = %d", reviewtarget.MaxSourceProofLines, MaxSourceLines)
	}
}
