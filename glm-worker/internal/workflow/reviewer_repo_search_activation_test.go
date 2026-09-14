package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) reviewerDiffFirstContext(request string, reviewNumber int) string {
	block, err := w.reviewerDiffFirstNavigation(request, reviewNumber)
	if err != nil {
		panic(err)
	}
	return block
}

func TestReviewerDiffFirstNavigationRejectsMalformedActivationPin(t *testing.T) {
	w, st, _ := newReviewerSearchWorkflow(t, []string{state.ParentPlanFile})
	if err := st.Write(repositoryharness.ActivationStateKey, "malformed"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		calls++
		return reposearch.Report{}, nil
	}

	if _, err := w.reviewerDiffFirstNavigation("review malformed activation", 1); err == nil {
		t.Fatal("malformed repository harness activation unexpectedly entered reviewer navigation")
	} else if !strings.Contains(err.Error(), "activation pin") {
		t.Fatalf("unexpected activation error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("repo search ran after malformed activation: %d calls", calls)
	}
}

func TestReviewerDiffFirstNavigationRejectsInactivePinForActiveTask(t *testing.T) {
	w, st, _ := newReviewerSearchWorkflow(t, []string{state.ParentPlanFile})
	if err := st.Write(activeTaskStateKey, activeTaskRepoPath); err != nil {
		t.Fatal(err)
	}
	pinRepositoryHarnessInactiveT(t, st)

	if _, err := w.reviewerDiffFirstNavigation("review mismatched activation", 1); err == nil {
		t.Fatal("inactive repository harness activation for an active task unexpectedly entered reviewer navigation")
	} else if !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("unexpected activation mismatch error: %v", err)
	}
}
