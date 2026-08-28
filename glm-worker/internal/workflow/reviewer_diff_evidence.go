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
	diffPath, err := w.captureReviewerTaskDiff()
	if err != nil {
		return "", err
	}
	exhaustive, err := w.exhaustiveSearchContext(request, activeTaskPath, state.ReviewerRole, reviewNumber+1)
	if err != nil {
		return "", err
	}
	return renderReviewerTaskDiffEvidence(diffPath) + "\n" + w.reviewerDiffFirstContext(request, reviewNumber) + exhaustive, nil
}

func renderReviewerTaskDiffEvidence(path string) string {
	return "REVIEW_CURRENT_TASK_DIFF:\nPATCH: " + path + "\nAUTHORITY: wrapper-baseline-to-review-start\nREAD_FIRST: true\nEND_REVIEW_CURRENT_TASK_DIFF"
}

func (w *Workflow) captureReviewerTaskDiff() (string, error) {
	baseline, err := w.state.Read("baseline-head")
	if err != nil {
		return "", fmt.Errorf("read review diff baseline head: %w", err)
	}
	if strings.TrimSpace(baseline) == "" {
		return "", errors.New("review diff baseline head is empty")
	}
	indexPatch, err := w.state.Read("baseline-index.patch")
	if err != nil {
		return "", fmt.Errorf("read baseline index patch: %w", err)
	}
	worktreePatch, err := w.state.Read("baseline-worktree.patch")
	if err != nil {
		return "", fmt.Errorf("read baseline worktree patch: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "glm-worker-review-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)
	indexPath := filepath.Join(tempDir, "index")
	if _, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "read-tree", strings.TrimSpace(baseline)); err != nil {
		return "", fmt.Errorf("reconstruct review baseline index: %w", err)
	}
	for _, patch := range []string{indexPatch, worktreePatch} {
		if strings.TrimSpace(patch) == "" {
			continue
		}
		if _, err := gitWithIndex(w.config.RepoRoot, indexPath, strings.NewReader(patch), "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
			return "", fmt.Errorf("reconstruct review baseline worktree: %w", err)
		}
	}

	diff, err := gitWithIndex(w.config.RepoRoot, indexPath, nil, "diff", "--binary", "--no-ext-diff", "--no-renames", "--")
	if err != nil {
		return "", fmt.Errorf("capture tracked review task diff: %w", err)
	}
	untracked, err := taskCreatedUntrackedDiff(w.config.RepoRoot, w.state.ReadOr("baseline-untracked", ""))
	if err != nil {
		return "", err
	}
	diff = append(diff, untracked...)
	if err := w.state.Write(reviewerTaskDiffFile, string(diff)); err != nil {
		return "", err
	}
	return w.state.Path(reviewerTaskDiffFile), nil
}

func gitWithIndex(repoRoot, indexPath string, stdin *strings.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func taskCreatedUntrackedDiff(repoRoot, baselineRaw string) ([]byte, error) {
	currentRaw, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("list current untracked files: %w", err)
	}
	baseline := nulPathSet([]byte(baselineRaw))
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
