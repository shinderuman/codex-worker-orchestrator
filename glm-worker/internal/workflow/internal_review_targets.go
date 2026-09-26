package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

const (
	internalReviewNoTarget     = "none"
	internalReviewPacketTarget = "PACKET"
)

func canonicalReviewTargets(values []string) []string {
	targets := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || value == internalReviewNoTarget || value == internalReviewPacketTarget {
			continue
		}
		target := value
		if _, _, err := reviewtarget.Parse(target); err != nil {
			if !strings.Contains(value, "/") && !strings.Contains(value, ".") {
				continue
			}
			target = fmt.Sprintf("%s:%s", value, reviewtarget.WholeFileDiffLocator)
			if _, _, parseErr := reviewtarget.Parse(target); parseErr != nil {
				continue
			}
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets
}

func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
	if w.collectChangedPaths == nil {
		return nil, fmt.Errorf("current task review targets: changed-path collector is unavailable")
	}
	paths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
	if err != nil {
		return nil, fmt.Errorf("current task review targets: %w", err)
	}
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	targets := canonicalReviewTargets(paths)
	if len(targets) == 0 {
		return nil, fmt.Errorf("current task review targets: current task has no changed paths")
	}
	return targets, nil
}
