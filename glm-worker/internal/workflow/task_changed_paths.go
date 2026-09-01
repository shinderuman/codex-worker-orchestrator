package workflow

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

const conservativeEmptyTreeObject = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func collectExactTaskChangedPaths(repoRoot string, st *state.StateStore) ([]string, error) {
	paths, available, err := taskdiff.ChangedPaths(repoRoot, st)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, fmt.Errorf("captured task baseline is unavailable")
	}
	return paths, nil
}

func collectTaskChangedPaths(repoRoot string, st *state.StateStore) ([]string, error) {
	exact, err := collectExactTaskChangedPaths(repoRoot, st)
	if err != nil {
		return nil, err
	}
	expansion, err := collectConservativeChangedPathExpansion(repoRoot, st.ReadOr("baseline-head", ""))
	if err != nil {
		return nil, err
	}
	return appendUniqueChangedPaths(expansion, exact...), nil
}

func collectConservativeChangedPathExpansion(repoRoot, baselineHead string) ([]string, error) {
	base := strings.TrimSpace(baselineHead)
	if base == "" {
		base = conservativeEmptyTreeObject
	}
	tracked, err := exec.Command("git", "-C", repoRoot, "diff", "--no-renames", "--name-only", "-z", base).Output()
	if err != nil {
		return nil, fmt.Errorf("conservative changed-path expansion git diff: %w", err)
	}
	untracked, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("conservative changed-path expansion git ls-files: %w", err)
	}
	paths := splitChangedPathNul(tracked)
	paths = appendUniqueChangedPaths(paths, splitChangedPathNul(untracked)...)
	return paths, nil
}

func appendUniqueChangedPaths(paths []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(paths)+len(additions))
	for _, path := range paths {
		seen[path] = struct{}{}
	}
	for _, path := range additions {
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func splitChangedPathNul(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func collectChangedPaths(string, string) ([]string, error) {
	return nil, fmt.Errorf("changed-path collection requires the captured task baseline state")
}
