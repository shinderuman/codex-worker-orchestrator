package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type reviewerDiffBaseline struct {
	head          string
	indexPatch    []byte
	worktreePatch []byte
}

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
	baseline, available, err := w.loadReviewerDiffBaseline()
	if err != nil || !available {
		return "", available, err
	}
	indexPath, cleanup, err := w.reconstructReviewerBaselineIndex(baseline)
	if err != nil {
		return "", false, err
	}
	defer cleanup()
	diff, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "diff", "--binary", "--no-ext-diff", "--no-renames", "--")
	if err != nil {
		return "", false, fmt.Errorf("capture tracked review task diff: %w", err)
	}
	diff, err = w.appendReviewerUntrackedDiff(diff)
	if err != nil {
		return "", false, err
	}
	if err := w.state.Write(reviewerTaskDiffFile, string(diff)); err != nil {
		return "", false, err
	}
	return w.state.Path(reviewerTaskDiffFile), true, nil
}

func (w *Workflow) loadReviewerDiffBaseline() (reviewerDiffBaseline, bool, error) {
	if !w.state.Exists("baseline-head") || !w.state.Exists("baseline-status") {
		return reviewerDiffBaseline{}, false, nil
	}
	head, err := w.state.Read("baseline-head")
	if err != nil {
		return reviewerDiffBaseline{}, false, fmt.Errorf("read review diff baseline head: %w", err)
	}
	if strings.TrimSpace(head) == "" {
		return reviewerDiffBaseline{}, false, nil
	}
	indexPatch, err := os.ReadFile(w.state.Path("baseline-index.patch"))
	if err != nil {
		return reviewerDiffBaseline{}, false, fmt.Errorf("read baseline index patch: %w", err)
	}
	worktreePatch, err := os.ReadFile(w.state.Path("baseline-worktree.patch"))
	if err != nil {
		return reviewerDiffBaseline{}, false, fmt.Errorf("read baseline worktree patch: %w", err)
	}
	return reviewerDiffBaseline{head: strings.TrimSpace(head), indexPatch: indexPatch, worktreePatch: worktreePatch}, true, nil
}

func (w *Workflow) reconstructReviewerBaselineIndex(baseline reviewerDiffBaseline) (string, func(), error) {
	tempDir, err := os.MkdirTemp(os.Getenv("GLM_WORKER_GIT_TEMP_ROOT"), "glm-worker-review-diff-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	indexPath := filepath.Join(tempDir, "index")
	if _, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "read-tree", baseline.head); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("reconstruct review baseline index: %w", err)
	}
	for _, patch := range [][]byte{baseline.indexPatch, baseline.worktreePatch} {
		if len(bytes.TrimSpace(patch)) == 0 {
			continue
		}
		if _, err := gitWithIndex(w.config.RepoRoot, indexPath, patch, "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("reconstruct review baseline worktree: %w", err)
		}
	}
	return indexPath, cleanup, nil
}

func (w *Workflow) appendReviewerUntrackedDiff(diff []byte) ([]byte, error) {
	if !w.state.Exists("baseline-untracked") {
		return diff, nil
	}
	baselineUntracked, err := os.ReadFile(w.state.Path("baseline-untracked"))
	if err != nil {
		return nil, fmt.Errorf("read baseline untracked paths: %w", err)
	}
	untracked, err := taskCreatedUntrackedDiff(w.config.RepoRoot, baselineUntracked)
	if err != nil {
		return nil, err
	}
	return append(diff, untracked...), nil
}

func gitWithIndex(repoRoot, indexPath string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func taskCreatedUntrackedDiff(repoRoot string, baselineRaw []byte) ([]byte, error) {
	currentRaw, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("list current untracked files: %w", err)
	}
	baseline := nulPathSet(baselineRaw)
	var result bytes.Buffer
	for _, path := range splitNul(currentRaw) {
		if _, existed := baseline[path]; existed {
			continue
		}
		patch, err := untrackedFilePatch(repoRoot, path)
		if err != nil {
			return nil, err
		}
		result.Write(patch)
	}
	return result.Bytes(), nil
}

func untrackedFilePatch(repoRoot, path string) ([]byte, error) {
	cmd := exec.Command("git", "-C", repoRoot, "diff", "--no-index", "--binary", "--", "/dev/null", path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return output, nil
	}
	return nil, fmt.Errorf("capture untracked review diff for %s: %w: %s", path, err, strings.TrimSpace(string(output)))
}

func nulPathSet(raw []byte) map[string]struct{} {
	result := make(map[string]struct{})
	for _, path := range splitNul(raw) {
		result[path] = struct{}{}
	}
	return result
}
