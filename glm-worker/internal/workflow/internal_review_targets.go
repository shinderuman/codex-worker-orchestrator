package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

const (
	internalReviewNoTarget     = "none"
	internalReviewPacketTarget = "PACKET"
)

func canonicalReviewTargets(values []string) ([]string, error) {
	targets := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || value == internalReviewNoTarget || value == internalReviewPacketTarget {
			continue
		}
		if _, err := reviewtarget.ParseTarget(value); err != nil {
			return nil, fmt.Errorf("review target %q is not canonical: %w", value, err)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		targets = append(targets, value)
	}
	return targets, nil
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
	targets := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		target := fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator)
		parsed, err := reviewtarget.ParseTarget(target)
		if err != nil {
			return nil, fmt.Errorf("current task review targets: changed path %q cannot be represented as a review target: %w", path, err)
		}
		if err := reviewtarget.ValidateProofAddressable(w.config.RepoRoot, parsed); err != nil {
			return nil, fmt.Errorf("current task review targets: changed path %q has no whole-diff proof: %w", path, err)
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

func (w *Workflow) resultOrCurrentReviewTargets(result packet.Result) ([]string, error) {
	targets, err := canonicalReviewTargets(result.Targets)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return w.currentReviewDiffTargets()
	}
	for _, raw := range targets {
		target, err := reviewtarget.ParseTarget(raw)
		if err != nil {
			return nil, err
		}
		if err := reviewtarget.ValidateProofAddressable(w.config.RepoRoot, target); err != nil {
			return nil, fmt.Errorf("review target %q has no proof path: %w", raw, err)
		}
	}
	return targets, nil
}
