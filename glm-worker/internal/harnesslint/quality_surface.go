package harnesslint

import (
	"strings"
)

type qualityWiringCheck struct {
	path          string
	tokens        []string
	orderedTokens []string
}

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
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[path] = true
	}
	var violations []Violation
	workflowViolations, err := qualityWiringPackageViolations(root, present, "glm-worker/internal/workflow/", []string{
		"w.captureQualitySurfaceBaseline()",
		"w.verifyQualitySurfaceBaseline(workerPhase)",
		"w.qualityGate(w.config.RepoRoot)",
		"harnesslint.IsViolation(qualityReport)",
	})
	if err != nil {
		return nil, err
	}
	violations = append(violations, workflowViolations...)
	for _, check := range qualityWiringChecks() {
		current, err := qualityWiringCheckViolations(root, present, check)
		if err != nil {
			return nil, err
		}
		violations = append(violations, current...)
	}
	return violations, nil
}

func qualityWiringChecks() []qualityWiringCheck {
	checks := []qualityWiringCheck{
		{
			path: "glm-worker/internal/workflow/quality_gate.go",
			tokens: []string{
				"harnesslint.Run(root, true)",
				"harnesslint.Check(root)",
				"captureQualitySurfaceDigest",
			},
			orderedTokens: []string{
				"harnesslint.Run(root, true)",
				"harnesslint.Check(root)",
			},
		},
		{
			path: "install.sh",
			tokens: []string{
				"quality-tools.yml",
				"QUALITY_TOOLS_BIN_DIR",
				"quality_tool_path",
				"require_quality_tool",
				"./cmd/harnesslint",
				"./cmd/plancheck",
				"./cmd/repo-cli-install",
				"\"$build_dir/repo-cli-install\" -mode install -build-dir \"$build_dir\" -bin-dir \"$bin_dir\"",
				"result=$(\"$build_dir/merge-json\" -fragment \"$repo_root/claude/settings-managed.json\")",
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
				"quality-tools.yml",
				"GOTOOLCHAIN",
				"GOCACHE",
				"run ./cmd/harnesslint",
			},
		},
	}
	return append(checks, qualityToolWiringChecks()...)
}

func qualityToolWiringChecks() []qualityWiringCheck {
	return []qualityWiringCheck{
		{
			path: "quality-tools.yml",
			tokens: []string{
				"namespace:",
				"default-bin-dir:",
				"go:",
				"lint-go:",
				"golangci-lint:",
				"shellcheck:",
				"shfmt:",
			},
		},
		{
			path: ".github/workflows/quality.yml",
			tokens: []string{
				"quality-tools.yml",
				"quality-tools.outputs.go_version",
				"QUALITY_TOOLS_BIN_DIR",
				"$GITHUB_ENV",
				"./install-quality-tools.sh",
				"./tests/install_quality_tools_smoke.sh",
			},
		},
		{
			path: "install-quality-tools.sh",
			tokens: []string{
				"quality-tools.yml",
				"QUALITY_TOOLS_BIN_DIR",
				"tool_namespace=$(contract_value namespace)",
				"default_bin_dir=$(contract_value default-bin-dir)",
				"quality_tool_path",
				"target_needs_install",
				"golangci-lint/releases/download",
				"shellcheck/releases/download",
				"go install",
			},
		},
		{
			path: "tests/install_quality_tools_smoke.sh",
			tokens: []string{
				"user-owned-$tool",
				"codex-worker-orchestrator-shfmt-3.13.1",
				"codex-worker-orchestrator-shfmt-3.13.2",
				"quality tool collision:",
			},
		},
	}
}

func qualityWiringPackageViolations(root string, present map[string]bool, prefix string, tokens []string) ([]Violation, error) {
	found := make(map[string]bool, len(tokens))
	packageFound := false
	for path := range present {
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		packageFound = true
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		text := string(data)
		for _, token := range tokens {
			if strings.Contains(text, token) {
				found[token] = true
			}
		}
	}
	path := strings.TrimSuffix(prefix, "/")
	if !packageFound {
		return []Violation{{Rule: "quality-wiring", Path: path, Line: 1, Column: 1, Message: "required quality-gate package is missing"}}, nil
	}
	var violations []Violation
	for _, token := range tokens {
		if found[token] {
			continue
		}
		violations = append(violations, Violation{
			Rule: "quality-wiring", Path: path, Line: 1, Column: 1,
			Message: "required quality-gate wiring is missing: " + token,
		})
	}
	return violations, nil
}

func qualityWiringCheckViolations(root string, present map[string]bool, check qualityWiringCheck) ([]Violation, error) {
	if !present[check.path] {
		return []Violation{{
			Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
			Message: "required quality-gate file is missing",
		}}, nil
	}
	data, err := readRegularFile(root, check.path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	var violations []Violation
	for _, token := range check.tokens {
		if strings.Contains(text, token) {
			continue
		}
		violations = append(violations, Violation{
			Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
			Message: "required quality-gate wiring is missing: " + token,
		})
	}
	lastIndex := -1
	for _, token := range check.orderedTokens {
		index := strings.Index(text, token)
		if index < 0 {
			continue
		}
		if index <= lastIndex {
			violations = append(violations, Violation{
				Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
				Message: "required quality-gate wiring order is invalid: " + strings.Join(check.orderedTokens, " -> "),
			})
			break
		}
		lastIndex = index
	}
	return violations, nil
}
