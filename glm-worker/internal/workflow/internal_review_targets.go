package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
	collect := w.collectChangedPaths
	if w.collectReviewTargetPaths != nil {
		collect = w.collectReviewTargetPaths
	}
	if collect == nil {
		return nil, fmt.Errorf("current task review targets: changed-path collector is unavailable")
	}
	paths, err := collect(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
	if err != nil {
		return nil, fmt.Errorf("current task review targets: %w", err)
	}
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	targets := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		target := fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator)
		if _, _, err := reviewtarget.Parse(target); err != nil {
			return nil, fmt.Errorf("current task review targets: changed path %q cannot be represented as a review target: %w", path, err)
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("current task review targets: current task has no changed paths")
	}
	return targets, nil
}
