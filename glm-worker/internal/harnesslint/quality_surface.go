package harnesslint

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

var qualityPathspecs = []string{
	".golangci.yml",
	"commentlint",
	"harnesslint",
	"glm-worker/cmd/commentlint",
	"glm-worker/cmd/harnesslint",
	"glm-worker/internal/commentlint",
	"glm-worker/internal/harnesslint",
}

func scanQualitySurface(root string, paths []string) ([]Violation, error) {
	dirty, err := dirtyQualitySurface(root)
	if err != nil {
		return nil, err
	}
	wiring, err := qualityWiringViolations(root, paths)
	if err != nil {
		return nil, err
	}
	streams, err := processStreamViolations(root, paths)
	if err != nil {
		return nil, err
	}
	violations := append(dirty, wiring...)
	return append(violations, streams...), nil
}

func dirtyQualitySurface(root string) ([]Violation, error) {
	if err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run(); err != nil {
		return nil, nil
	}
	args := []string{"-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--"}
	args = append(args, qualityPathspecs...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git status quality surface: %w", err)
	}
	var violations []Violation
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) < 4 {
			continue
		}
		path := filepath.ToSlash(string(record[3:]))
		violations = append(violations, Violation{
			Rule: "quality-surface-dirty", Path: path, Line: 1, Column: 1,
			Message: "quality policy surface must not be modified by the worker task; report a concrete policy change request instead",
		})
	}
	return violations, nil
}

func qualityWiringViolations(root string, paths []string) ([]Violation, error) {
	checks := []struct {
		path   string
		tokens []string
	}{
		{
			path: "glm-worker/internal/workflow/workflow.go",
			tokens: []string{
				"return commentlint.Check(root)",
				"w.commentLint(w.config.RepoRoot)",
				"commentlint.IsViolation(commentReport)",
			},
		},
		{
			path: "install.sh",
			tokens: []string{
				"./cmd/harnesslint",
				"for name in glm-worker commentlint harnesslint merge-json",
			},
		},
		{
			path: "harnesslint",
			tokens: []string{
				"glm-worker/cmd/harnesslint",
			},
		},
	}
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[path] = true
	}
	var violations []Violation
	for _, check := range checks {
		if !present[check.path] {
			continue
		}
		data, err := readRegularFile(root, check.path)
		if err != nil {
			return nil, err
		}
		text := string(data)
		for _, token := range check.tokens {
			if strings.Contains(text, token) {
				continue
			}
			violations = append(violations, Violation{
				Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
				Message: "required quality-gate wiring is missing: " + token,
			})
		}
	}
	return violations, nil
}
