package harnesslint

type qualityWiringCheck struct {
	path            string
	tokens          []string
	forbiddenTokens []string
	orderedTokens   []string
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
				"captureQualitySurfaceDigest",
			},
			forbiddenTokens: []string{
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
				"DEADCODE_VERSION=$(quality_contract_value deadcode)",
				"require_quality_tool deadcode \"$DEADCODE_VERSION\"",
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
				"deadcode:",
				"shellcheck:",
				"shfmt:",
			},
		},
		ciWorkflowWiringCheck(),
		installSmokeWorkflowWiringCheck(),
		{
			path: "install-quality-tools.sh",
			tokens: []string{
				"quality-tools.yml",
				"QUALITY_TOOLS_BIN_DIR",
				"tool_namespace=$(contract_value namespace)",
				"default_bin_dir=$(contract_value default-bin-dir)",
				"deadcode_version=$(contract_value deadcode)",
				"quality_tool_path",
				"target_needs_install",
				"golang.org/x/tools/cmd/deadcode@v$deadcode_version",
				"golangci-lint/releases/download",
				"shellcheck/releases/download",
				"go install",
			},
		},
		{
			path: "tests/install_quality_tools_smoke.sh",
			tokens: []string{
				"user-owned-$tool",
				"codex-worker-orchestrator-deadcode-0.49.0",
				"codex-worker-orchestrator-shfmt-3.13.1",
				"codex-worker-orchestrator-shfmt-3.13.2",
				"quality tool collision:",
			},
		},
	}
}

func ciWorkflowWiringCheck() qualityWiringCheck {
	return qualityWiringCheck{
		path: ".github/workflows/ci.yml",
		tokens: []string{
			"quality-tools.yml",
			"quality-tools.outputs.go_version",
			"QUALITY_TOOLS_BIN_DIR",
			"$GITHUB_ENV",
			"./install-quality-tools.sh",
			"./tests/install_quality_tools_smoke.sh",
			"./.github/workflows/install-smoke.yml",
		},
	}
}

func installSmokeWorkflowWiringCheck() qualityWiringCheck {
	return qualityWiringCheck{
		path: ".github/workflows/install-smoke.yml",
		tokens: []string{
			"workflow_call:",
			"quality-tools.yml",
			"quality-tools.outputs.go_version",
			"QUALITY_TOOLS_BIN_DIR",
			"$GITHUB_ENV",
			"./install-quality-tools.sh",
			"./tests/install_smoke.sh",
			"./tests/install_detached_runtime_identity_smoke.sh",
		},
	}
}
