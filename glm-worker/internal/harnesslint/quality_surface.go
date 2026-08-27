package harnesslint

import (
	"strings"
)

func scanQualitySurface(root string, paths []string) ([]Violation, error) {
	wiring, err := qualityWiringViolations(root, paths)
	if err != nil {
		return nil, err
	}
	streams, err := processStreamViolations(root, paths)
	if err != nil {
		return nil, err
	}
	return append(wiring, streams...), nil
}

func qualityWiringViolations(root string, paths []string) ([]Violation, error) {
	checks := []struct {
		path   string
		tokens []string
	}{
		{
			path: "glm-worker/internal/workflow/workflow.go",
			tokens: []string{
				"w.captureQualitySurfaceBaseline()",
				"w.verifyQualitySurfaceBaseline(workerPhase)",
				"w.qualityGate(w.config.RepoRoot)",
				"harnesslint.IsViolation(qualityReport)",
			},
		},
		{
			path: "glm-worker/internal/workflow/quality_gate.go",
			tokens: []string{
				"harnesslint.Run(root, true)",
				"harnesslint.Check(root)",
				"captureQualitySurfaceDigest",
			},
		},
		{
			path: "install.sh",
			tokens: []string{
				"./cmd/harnesslint",
				"./cmd/plancheck",
				"for name in glm-worker commentlint harnesslint merge-json",
				"\"$build_dir/plancheck\" \"$repo_root\"",
			},
		},
		{
			path: ".githooks/post-merge",
			tokens: []string{
				"exec \"$repo_root/install.sh\"",
			},
		},
		{
			path: "harnesslint",
			tokens: []string{
				"run ./cmd/harnesslint",
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
			violations = append(violations, Violation{
				Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
				Message: "required quality-gate file is missing",
			})
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
