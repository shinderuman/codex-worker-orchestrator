package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: glm-parent-action prepare <decision|fix> | <decision|fix> <token> [fix options]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "prepare" {
		if len(args) != 2 {
			return fmt.Errorf("usage: glm-parent-action prepare <decision|fix>")
		}
		prepared, err := parentaction.Prepare(cfg.RepoRoot, args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Status string `json:"status"`
			parentaction.Prepared
		}{Status: "prepared", Prepared: prepared})
	}

	action := args[0]
	if action != "decision" && action != "fix" {
		return fmt.Errorf("parent action must be decision or fix")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: glm-parent-action %s <token> [fix options]", action)
	}
	if action == "decision" && len(args) != 2 {
		return fmt.Errorf("usage: glm-parent-action decision <token>")
	}
	if action == "fix" && (len(args)-2)%2 != 0 {
		return fmt.Errorf("usage: glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff]")
	}

	payload, err := parentaction.Consume(cfg.RepoRoot, action, args[1])
	if err != nil {
		return err
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	mode := "--decision-stdin"
	if action == "fix" {
		mode = "--fix-stdin"
	}
	workerArgs := []string{mode, strconv.Itoa(len(payload)), "--sha256", hex.EncodeToString(digest[:])}
	workerArgs = append(workerArgs, args[2:]...)
	command := exec.Command(worker, workerArgs...)
	command.Dir = cfg.RepoRoot
	command.Stdin = bytes.NewReader(payload)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func resolveGLMWorker() (string, error) {
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "glm-worker")
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	worker, err := exec.LookPath("glm-worker")
	if err != nil {
		return "", fmt.Errorf("glm-worker executable not found: %w", err)
	}
	return worker, nil
}
