package parentactioncmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout, stderr); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: glm-parent-action prepare <decision|fix> | <decision|fix> <token> [fix options]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "prepare" {
		return prepare(cfg.RepoRoot, args, stdout)
	}
	return execute(cfg.RepoRoot, args, stdout, stderr)
}

func prepare(repoRoot string, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: glm-parent-action prepare <decision|fix>")
	}
	prepared, err := parentaction.Prepare(repoRoot, args[1])
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(struct {
		Status string `json:"status"`
		parentaction.Prepared
	}{Status: "prepared", Prepared: prepared})
}

func execute(repoRoot string, args []string, stdout, stderr io.Writer) error {
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
	if action == "fix" {
		if err := validateFixOptions(args[2:]); err != nil {
			return err
		}
	}

	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	payload, err := parentaction.Consume(repoRoot, action, args[1])
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
	command.Dir = repoRoot
	command.Stdin = bytes.NewReader(payload)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func validateFixOptions(options []string) error {
	if len(options)%2 != 0 {
		return fmt.Errorf("usage: glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff]")
	}
	seen := map[string]bool{}
	for index := 0; index < len(options); index += 2 {
		name := options[index]
		if seen[name] || (name != "--origin" && name != "--accepted-scope") {
			return fmt.Errorf("usage: glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff]")
		}
		seen[name] = true
		if options[index+1] == "" {
			return fmt.Errorf("usage: glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff]")
		}
	}
	return nil
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
