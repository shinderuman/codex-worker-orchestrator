package repositoryprojecttree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildTaskAttributionRejectsMalformedActiveSection(t *testing.T) {
	root := t.TempDir()
	plan := "# plan\n\n## ACTIVE\n\nnot-a-schedule-entry\n"
	if err := os.WriteFile(filepath.Join(root, state.ParentPlanFile), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := BuildTaskAttribution(root, "IMPLEMENTATION_TASKS/stale.md", repositoryproject.Continuation{})
	if err == nil || !strings.Contains(err.Error(), "schedule list記法") {
		t.Fatalf("malformed ACTIVE was not rejected: %v", err)
	}
}
