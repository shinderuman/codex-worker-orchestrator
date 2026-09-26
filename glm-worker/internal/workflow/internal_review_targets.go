package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

const internalReviewNoTarget = "none"

func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
	paths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
	if err != nil {
		return nil, fmt.Errorf("collect current review targets: %w", err)
	}
	return canonicalReviewTargets(paths)
}

func canonicalReviewTargets(values []string) ([]string, error) {
	targets := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || value == internalReviewNoTarget {
			continue
		}
		target := value
		if _, _, err := reviewtarget.Parse(target); err != nil {
			target = fmt.Sprintf("%s:%s", value, reviewtarget.WholeFileDiffLocator)
			if _, _, parseErr := reviewtarget.Parse(target); parseErr != nil {
				return nil, fmt.Errorf("review target %q is not repository-addressable: %w", value, parseErr)
			}
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("review target set is empty")
	}
	return targets, nil
}

func (w *Workflow) reviewTargetsOrCurrent(values []string) ([]string, error) {
	if targets, err := canonicalReviewTargets(values); err == nil {
		return targets, nil
	}
	return w.currentReviewDiffTargets()
}

func (w *Workflow) canonicalizeInternalReviewResult(value packet.Result) (packet.Result, error) {
	if value.Status != packet.StatusNeedsSolReview {
		return value, nil
	}
	targets, err := w.reviewTargetsOrCurrent(value.Targets)
	if err != nil {
		return packet.Result{}, err
	}
	value.Targets = targets
	return value, nil
}
