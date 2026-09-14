package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) reviewerDiffFirstContext(request string, reviewNumber int) string {
	parentMetadataFilterActive, err := RepositoryHarnessActive(w.config.RepoRoot, w.state)
	if err != nil {
		panic(err)
	}
	block, err := w.reviewerDiffFirstNavigationWithHarness(request, reviewNumber, parentMetadataFilterActive)
	if err != nil {
		panic(err)
	}
	return block
}

func TestReviewerNavigationContextRejectsMalformedActivationBeforeExhaustiveSearch(t *testing.T) {
	w, st, taskID := newReviewerSearchWorkflow(t, []string{state.ParentPlanFile})
	if err := st.Write(repositoryharness.ActivationStateKey, "malformed"); err != nil {
		t.Fatal(err)
	}

	request := "review malformed activation\n" + exhaustiveSearchRequiredMarker
	if _, err := w.reviewerNavigationContext(request, "", 1); err == nil {
		t.Fatal("malformed repository harness activation unexpectedly entered reviewer navigation")
	} else if !strings.Contains(err.Error(), "activation pin") {
		t.Fatalf("unexpected activation error: %v", err)
	}

	manifestPath := st.Path(filepath.Join("artifacts", taskID, exhaustiveSearchManifestDir, "reviewer-2.txt"))
	if _, err := os.Stat(manifestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exhaustive search manifest was written before activation validation: %v", err)
	}
	if _, err := os.Stat(st.TaskEventLogPath(taskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exhaustive search telemetry was written before activation validation: %v", err)
	}
}

func TestReviewerNavigationContextRejectsInactivePinForActiveTask(t *testing.T) {
	w, st, _ := newReviewerSearchWorkflow(t, []string{state.ParentPlanFile})
	if err := st.Write(activeTaskStateKey, activeTaskRepoPath); err != nil {
		t.Fatal(err)
	}
	pinRepositoryHarnessInactiveT(t, st)

	if _, err := w.reviewerNavigationContext("review mismatched activation", "", 1); err == nil {
		t.Fatal("inactive repository harness activation for an active task unexpectedly entered reviewer navigation")
	} else if !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("unexpected activation mismatch error: %v", err)
	}
}
