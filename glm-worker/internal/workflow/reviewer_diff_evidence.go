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
	if !w.state.Exists("baseline-head") || !w.state.Exists("baseline-status") {
		return "", false, nil
	}
	baseline, err := w.state.Read("baseline-head")
	if err != nil {
		return "", false, fmt.Errorf("read review diff baseline head: %w", err)
	}
	if strings.TrimSpace(baseline) == "" {
		return "", false, nil
	}
	indexPatch, err := os.ReadFile(w.state.Path("baseline-index.patch"))
	if err != nil {
		return "", false, fmt.Errorf("read baseline index patch: %w", err)
	}
	worktreePatch, err := os.ReadFile(w.state.Path("baseline-worktree.patch"))
	if err != nil {
		return "", false, fmt.Errorf("read baseline worktree patch: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "glm-worker-review-diff-")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	indexPath := filepath.Join(tempDir, "index")
	if _, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "read-tree", strings.TrimSpace(baseline)); err != nil {
		return "", false, fmt.Errorf("reconstruct review baseline index: %w", err)
	}
	for _, patch := range [][]byte{indexPatch, worktreePatch} {
		if len(bytes.TrimSpace(patch)) == 0 {
			continue
		}
		if _, err := gitWithIndex(w.config.RepoRoot, indexPath, patch, "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
			return "", false, fmt.Errorf("reconstruct review baseline worktree: %w", err)
		}
	}

	diff, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "diff", "--binary", "--no-ext-diff", "--no-renames", "--")
	if err != nil {
		return "", false, fmt.Errorf("capture tracked review task diff: %w", err)
	}
	if w.state.Exists("baseline-untracked") {
		baselineUntracked, err := os.ReadFile(w.state.Path("baseline-untracked"))
		if err != nil {
			return "", false, fmt.Errorf("read baseline untracked paths: %w", err)
		}
		untracked, err := taskCreatedUntrackedDiff(w.config.RepoRoot, baselineUntracked)
		if err != nil {
			return "", false, err
		}
		diff = append(diff, untracked...)
	}
	if err := w.state.Write(reviewerTaskDiffFile, string(diff)); err != nil {
		return "", false, err
	}
	return w.state.Path(reviewerTaskDiffFile), true, nil
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
