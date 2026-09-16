package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func PrintParentReviewEvidence(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	manifest, err := buildParentReviewEvidenceManifest(st)
	if err != nil {
		return err
	}
	ownerCallID, err := state.NewUUID()
	if err != nil {
		return err
	}
	projector := &parentEvidenceProjector{cfg: cfg, st: st, ownerCallID: ownerCallID}
	projector.project(manifest)
	return commitParentEvidenceProjection(projector, stdout, manifest.Reason)
}

func buildParentReviewEvidenceManifest(st *state.StateStore) (parentEvidenceManifest, error) {
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		return parentEvidenceManifest{}, err
	}
	if binding == nil {
		return parentEvidenceManifest{}, fmt.Errorf("review evidence requires an open NEEDS_SOL_REVIEW binding")
	}
	question := strings.TrimSpace(binding.SolQuestion)
	if question == "" || len(binding.Targets) == 0 {
		return parentEvidenceManifest{}, fmt.Errorf("review evidence binding has no semantic question or targets")
	}

	manifest := parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "parent-review-targets:" + binding.ID,
	}
	seenSource := map[string]struct{}{}
	diffPaths := map[string]struct{}{}
	for _, target := range binding.Targets {
		path, locator, err := parentReviewEvidenceTarget(target)
		if err != nil {
			return parentEvidenceManifest{}, err
		}
		if start, end, ok := parentReviewNumericRange(locator); ok {
			key := fmt.Sprintf("%s:%d-%d", path, start, end)
			if _, exists := seenSource[key]; exists {
				continue
			}
			seenSource[key] = struct{}{}
			manifest.Source = append(manifest.Source, parentEvidenceSourceRequest{
				Question:    question,
				Path:        path,
				LineStart:   start,
				LineEnd:     end,
				BudgetBytes: parentEvidenceMaxBudgetBytes,
			})
			continue
		}
		diffPaths[path] = struct{}{}
	}
	paths := make([]string, 0, len(diffPaths))
	for path := range diffPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		manifest.Diff = append(manifest.Diff, parentEvidenceDiffRequest{
			Question:    question,
			Paths:       []string{path},
			BudgetBytes: parentEvidenceMaxBudgetBytes,
		})
	}
	if err := validateParentEvidenceManifest(manifest); err != nil {
		return parentEvidenceManifest{}, err
	}
	return manifest, nil
}

func parentReviewEvidenceTarget(target string) (string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("review evidence target is empty")
	}
	path := target
	locator := ""
	if separator := strings.Index(target, ":"); separator >= 0 {
		path = strings.TrimSpace(target[:separator])
		locator = strings.TrimSpace(target[separator+1:])
	}
	if !parentEvidenceRelativePath(path) {
		return "", "", fmt.Errorf("review evidence target must start with a repository-relative path: %s", target)
	}
	if strings.ContainsAny(path, " ,()") {
		return "", "", fmt.Errorf("review evidence target path is not machine-addressable: %s", target)
	}
	return path, locator, nil
}
