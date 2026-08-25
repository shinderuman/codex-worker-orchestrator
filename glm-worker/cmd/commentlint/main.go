package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/commentlint"
)

type errorBody struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func main() {
	fix := false
	if len(os.Args) == 2 && os.Args[1] == "--fix" {
		fix = true
	} else if len(os.Args) != 1 {
		write(errorEnvelope{Error: errorBody{Kind: "usage", Message: "usage: commentlint [--fix]"}}, os.Stderr)
		os.Exit(2)
	}
	root, err := repositoryRoot()
	if err != nil {
		write(errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}}, os.Stderr)
		os.Exit(1)
	}
	if err := commentlint.ValidateRoot(root); err != nil {
		write(errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}}, os.Stderr)
		os.Exit(1)
	}
	report, err := commentlint.Run(root, fix)
	if err != nil {
		write(errorEnvelope{Error: errorBody{Kind: "internal", Message: err.Error()}}, os.Stderr)
		os.Exit(1)
	}
	write(report, os.Stdout)
	if commentlint.IsViolation(report) {
		os.Exit(1)
	}
}

func repositoryRoot() (string, error) {
	if root := os.Getenv("COMMENTLINT_REPO_ROOT"); root != "" {
		return root, nil
	}
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func write(value any, destination *os.File) {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
