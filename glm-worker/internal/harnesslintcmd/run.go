package harnesslintcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

type errorBody struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type runMode int

const (
	modeCheck runMode = iota
	modeFix
	modeDeterministicFix
	modeControlledCheck
)

func Run(args []string, stdout, stderr io.Writer) int {
	mode, ok := parseArgs(args)
	if !ok {
		write(stderr, errorEnvelope{Error: errorBody{Kind: "usage", Message: "usage: harnesslint [--fix|--deterministic-fix|--controlled-check]"}})
		return 2
	}
	root, err := repositoryRoot()
	if err != nil {
		write(stderr, errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}})
		return 1
	}
	report, err := runModeReport(root, mode)
	if err != nil {
		if mode == modeDeterministicFix {
			if failureReport, matched := harnesslint.DeterministicAutofixFailureReport(err); matched {
				write(stdout, failureReport)
				return deterministicAutofixFailureExit(failureReport)
			}
		}
		write(stderr, errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}})
		return 1
	}
	write(stdout, report)
	if harnesslint.IsViolation(report) {
		if mode == modeControlledCheck && harnesslint.HasFixableViolation(report) {
			return 3
		}
		return 1
	}
	return 0
}

func deterministicAutofixFailureExit(report harnesslint.Report) int {
	if report.DeterministicConvergence == nil {
		return 1
	}
	switch report.DeterministicConvergence.State {
	case harnesslint.DeterministicFixCycle:
		return 3
	case harnesslint.DeterministicFixBoundExhausted:
		return 4
	default:
		return 1
	}
}

func runModeReport(root string, mode runMode) (harnesslint.Report, error) {
	switch mode {
	case modeCheck:
		return harnesslint.Run(root, false)
	case modeFix:
		return harnesslint.Run(root, true)
	case modeDeterministicFix:
		controlRoot, err := controlledRoot()
		if err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.RunDeterministicAutofix(root, controlRoot)
	case modeControlledCheck:
		controlRoot, err := controlledRoot()
		if err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.RunControlled(root, controlRoot)
	default:
		return harnesslint.Report{}, fmt.Errorf("unknown harnesslint mode")
	}
}

func parseArgs(args []string) (runMode, bool) {
	switch {
	case len(args) == 0:
		return modeCheck, true
	case len(args) == 1 && args[0] == "--fix":
		return modeFix, true
	case len(args) == 1 && args[0] == "--deterministic-fix":
		return modeDeterministicFix, true
	case len(args) == 1 && args[0] == "--controlled-check":
		return modeControlledCheck, true
	default:
		return modeCheck, false
	}
}

func repositoryRoot() (string, error) {
	if root := os.Getenv("HARNESSLINT_REPO_ROOT"); root != "" {
		return root, nil
	}
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func controlledRoot() (string, error) {
	root := os.Getenv("HARNESSLINT_CONTROL_ROOT")
	if root == "" {
		return "", fmt.Errorf("HARNESSLINT_CONTROL_ROOT is required")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("HARNESSLINT_CONTROL_ROOT must be absolute")
	}
	return filepath.Clean(root), nil
}

func write(destination io.Writer, value any) {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
