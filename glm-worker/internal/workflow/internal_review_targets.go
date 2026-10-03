package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
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
		if _, err := reviewtarget.ParseTarget(value); err != nil {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		targets = append(targets, value)
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
	targets := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		target, err := proofableReviewPathTarget(w.config.RepoRoot, path)
		if err != nil {
			return nil, fmt.Errorf("current task review targets: %w", err)
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

func proofableReviewPathTarget(repoRoot, path string) (string, error) {
	diffRaw := fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator)
	diffTarget, err := reviewtarget.ParseTarget(diffRaw)
	if err != nil {
		return "", fmt.Errorf("changed path %q cannot be represented as a review target: %w", path, err)
	}
	diffErr := reviewtarget.ValidateProofAddressable(repoRoot, diffTarget)
	if diffErr == nil {
		return diffRaw, nil
	}

	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil {
		return "", fmt.Errorf("changed path %q has no canonical review proof path: %w", path, diffErr)
	}
	lineRaw := fmt.Sprintf("%s:1", path)
	lineTarget, err := reviewtarget.ParseTarget(lineRaw)
	if err != nil {
		return "", fmt.Errorf("changed path %q cannot be represented as source evidence: %w", path, err)
	}
	if err := reviewtarget.ValidateProofAddressable(repoRoot, lineTarget); err != nil {
		return "", fmt.Errorf("changed path %q has no canonical review proof path: %w", path, err)
	}
	return lineRaw, nil
}

func (w *Workflow) resultOrCurrentReviewTargets(result packet.Result) ([]string, error) {
	targets := canonicalReviewTargets(result.Targets)
	if len(targets) == 0 {
		return w.currentReviewDiffTargets()
	}
	for _, raw := range targets {
		target, err := reviewtarget.ParseTarget(raw)
		if err != nil || reviewtarget.ValidateProofAddressable(w.config.RepoRoot, target) != nil {
			return w.currentReviewDiffTargets()
		}
	}
	return targets, nil
}
