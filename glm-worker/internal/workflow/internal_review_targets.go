package workflow

import (
	"fmt"
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

func reviewTargetsOrFallback(values, fallback []string) []string {
	if targets := canonicalReviewTargets(values); len(targets) > 0 {
		return targets
	}
	return canonicalReviewTargets(fallback)
}

func (w *Workflow) currentReviewDiffTargetsOrFallback(fallback []string) []string {
	if w.collectChangedPaths != nil {
		paths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
		if err == nil {
			if targets := canonicalReviewTargets(paths); len(targets) > 0 {
				return targets
			}
		}
	}
	return canonicalReviewTargets(fallback)
}
