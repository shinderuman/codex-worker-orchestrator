package taskdiff

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

type baseline struct {
	head          string
	indexPatch    []byte
	worktreePatch []byte
}

func Capture(repoRoot string, st *state.StateStore) ([]byte, bool, error) {
	base, available, err := loadBaseline(st)
	if err != nil || !available {
		return nil, available, err
	}
	indexPath, cleanup, err := reconstructBaselineIndex(repoRoot, base)
	if err != nil {
		return nil, false, err
	}
	defer cleanup()

	diff, err := gitWithIndex(repoRoot, indexPath, nil, "diff", "--binary", "--no-ext-diff", "--no-renames", "--")
	if err != nil {
		return nil, false, fmt.Errorf("capture task diff: %w", err)
	}
	diff, err = appendUntrackedDiff(repoRoot, st, diff)
	if err != nil {
		return nil, false, err
	}
	return diff, true, nil
}

func loadBaseline(st *state.StateStore) (baseline, bool, error) {
	if !st.Exists("baseline-head") || !st.Exists("baseline-status") {
		return baseline{}, false, nil
	}
	head, err := st.Read("baseline-head")
	if err != nil {
		return baseline{}, false, fmt.Errorf("read task diff baseline head: %w", err)
	}
	if strings.TrimSpace(head) == "" {
		return baseline{}, false, nil
	}
	indexPatch, err := os.ReadFile(st.Path("baseline-index.patch"))
	if err != nil {
		return baseline{}, false, fmt.Errorf("read baseline index patch: %w", err)
	}
	worktreePatch, err := os.ReadFile(st.Path("baseline-worktree.patch"))
	if err != nil {
		return baseline{}, false, fmt.Errorf("read baseline worktree patch: %w", err)
	}
	return baseline{
		head:          strings.TrimSpace(head),
		indexPatch:    indexPatch,
		worktreePatch: worktreePatch,
	}, true, nil
}

func reconstructBaselineIndex(repoRoot string, base baseline) (string, func(), error) {
	tempDir, err := os.MkdirTemp(os.Getenv("GLM_WORKER_GIT_TEMP_ROOT"), "glm-worker-task-diff-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	indexPath := filepath.Join(tempDir, "index")
	if _, err := gitWithIndex(repoRoot, indexPath, nil, "read-tree", base.head); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("reconstruct task baseline index: %w", err)
	}
	for _, patch := range [][]byte{base.indexPatch, base.worktreePatch} {
		if len(bytes.TrimSpace(patch)) == 0 {
			continue
		}
		if _, err := gitWithIndex(repoRoot, indexPath, patch, "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("reconstruct task baseline worktree: %w", err)
		}
	}
	return indexPath, cleanup, nil
}

func appendUntrackedDiff(repoRoot string, st *state.StateStore, diff []byte) ([]byte, error) {
	if !st.Exists("baseline-untracked") {
		return diff, nil
	}
	baselineUntracked, err := os.ReadFile(st.Path("baseline-untracked"))
	if err != nil {
		return nil, fmt.Errorf("read baseline untracked paths: %w", err)
	}
	untracked, err := taskCreatedUntrackedDiff(repoRoot, baselineUntracked)
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
	existed := nulPathSet(baselineRaw)
	var result bytes.Buffer
	for _, filePath := range splitNul(currentRaw) {
		if _, ok := existed[filePath]; ok {
			continue
		}
		patch, err := untrackedFilePatch(repoRoot, filePath)
		if err != nil {
			return nil, err
		}
		result.Write(patch)
	}
	return result.Bytes(), nil
}

func untrackedFilePatch(repoRoot, filePath string) ([]byte, error) {
	cmd := exec.Command("git", "-C", repoRoot, "diff", "--no-index", "--binary", "--", "/dev/null", filePath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return output, nil
	}
	return nil, fmt.Errorf("capture untracked task diff for %s: %w: %s", filePath, err, strings.TrimSpace(string(output)))
}

func nulPathSet(raw []byte) map[string]struct{} {
	result := make(map[string]struct{})
	for _, filePath := range splitNul(raw) {
		result[filePath] = struct{}{}
	}
	return result
}

func splitNul(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
