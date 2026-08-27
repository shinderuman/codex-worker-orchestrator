package harnesslintcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func Run(args []string, stdout, stderr io.Writer) int {
	fix, ok := parseArgs(args)
	if !ok {
		write(stderr, errorEnvelope{Error: errorBody{Kind: "usage", Message: "usage: harnesslint [--fix]"}})
		return 2
	}
	root, err := repositoryRoot()
	if err != nil {
		write(stderr, errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}})
		return 1
	}
	report, err := harnesslint.Run(root, fix)
	if err != nil {
		write(stderr, errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}})
		return 1
	}
	write(stdout, report)
	if harnesslint.IsViolation(report) {
		return 1
	}
	return 0
}

func parseArgs(args []string) (bool, bool) {
	switch {
	case len(args) == 0:
		return false, true
	case len(args) == 1 && args[0] == "--fix":
		return true, true
	default:
		return false, false
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

func write(destination io.Writer, value any) {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
