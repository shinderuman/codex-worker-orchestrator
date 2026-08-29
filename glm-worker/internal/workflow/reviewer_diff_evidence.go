package workflow

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

const reviewerTaskDiffFile = "review-current-task.patch"

func (w *Workflow) reviewerNavigationContext(request, activeTaskPath string, reviewNumber int) (string, error) {
	diffPath, diffAvailable, err := w.captureReviewerTaskDiff()
	if err != nil {
		return "", err
	}
	exhaustive, err := w.exhaustiveSearchContext(request, activeTaskPath, state.ReviewerRole, reviewNumber+1)
	if err != nil {
		return "", err
	}
	return renderReviewerTaskDiffEvidence(diffPath, diffAvailable) + "\n" + w.reviewerDiffFirstContext(request, reviewNumber) + exhaustive, nil
}

func renderReviewerTaskDiffEvidence(path string, available bool) string {
	if !available {
		return "REVIEW_CURRENT_TASK_DIFF:\nPATCH: unavailable\nREAD_FIRST: false\nEND_REVIEW_CURRENT_TASK_DIFF"
	}
	return "REVIEW_CURRENT_TASK_DIFF:\nPATCH: " + path + "\nAUTHORITY: wrapper-baseline-to-review-start\nREAD_FIRST: true\nEND_REVIEW_CURRENT_TASK_DIFF"
}

func (w *Workflow) captureReviewerTaskDiff() (string, bool, error) {
	diff, available, err := taskdiff.Capture(w.config.RepoRoot, w.state)
	if err != nil || !available {
		return "", available, err
	}
	if err := w.state.Write(reviewerTaskDiffFile, string(diff)); err != nil {
		return "", false, err
	}
	return w.state.Path(reviewerTaskDiffFile), true, nil
}
