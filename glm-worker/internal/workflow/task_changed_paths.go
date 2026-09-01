package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

func collectTaskChangedPaths(repoRoot string, st *state.StateStore) ([]string, error) {
	paths, available, err := taskdiff.ChangedPaths(repoRoot, st)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, fmt.Errorf("captured task baseline is unavailable")
	}
	return paths, nil
}

func collectChangedPaths(string, string) ([]string, error) {
	return nil, fmt.Errorf("changed-path collection requires the captured task baseline state")
}
