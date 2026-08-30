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
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	usage             = "usage: glm-parent-action start | prepare <decision|fix> | decision <token> | fix <token> [--origin <origin>] [--accepted-scope current-diff] [--approval-only] | accept | resume | finalize-check <go-test|go-test-race>"
	activeTaskRequest = "現在のACTIVE taskを実行してください。"
	actionStart       = "start"
	actionFix         = "fix"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout, stderr); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return childExitCode(exitErr)
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "prepare" {
		return prepare(cfg.RepoRoot, args, stdout)
	}
	return execute(cfg, args, stdout, stderr)
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

func execute(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	action := args[0]
	switch action {
	case actionStart, "accept", "resume":
		if len(args) != 1 {
			return fmt.Errorf("usage: glm-parent-action %s", action)
		}
		if action == "resume" {
			if err := persistParentCodexIdentity(cfg); err != nil {
				return err
			}
		}
		return runWorker(cfg.RepoRoot, directWorkerArgs(action), nil, stdout, stderr, startIdentityEnv(action))
	case "decision", actionFix:
		if err := persistParentCodexIdentity(cfg); err != nil {
			return err
		}
		return executePayloadAction(cfg.RepoRoot, action, args[1:], stdout, stderr)
	case "finalize-check":
		if len(args) != 2 {
			return fmt.Errorf("usage: glm-parent-action finalize-check <go-test|go-test-race>")
		}
		validationDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve finalize-check working directory: %w", err)
		}
		return runFinalizationCheck(cfg.RepoRoot, validationDir, args[1], stdout)
	default:
		return fmt.Errorf("%s", usage)
	}
}

func startIdentityEnv(action string) []string {
	if action != actionStart {
		return nil
	}
	threadID, sessionID, ok := codexIdentityFromEnv()
	if !ok {
		return nil
	}
	return []string{
		state.ParentActionCodexThreadIDEnv + "=" + threadID,
		state.ParentActionCodexSessionIDEnv + "=" + sessionID,
	}
}

func persistParentCodexIdentity(cfg config.AppConfig) error {
	threadID, sessionID, ok := codexIdentityFromEnv()
	if !ok {
		return nil
	}
	return state.AttachStateStore(cfg).SetParentCodexIdentity(threadID, sessionID)
}

func codexIdentityFromEnv() (string, string, bool) {
	threadID := os.Getenv("CODEX_THREAD_ID")
	sessionID := os.Getenv("CODEX_SESSION_ID")
	if !state.ValidUUIDFormat(threadID) || !state.ValidUUIDFormat(sessionID) {
		return "", "", false
	}
	return threadID, sessionID, true
}

func executePayloadAction(repoRoot, action string, args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: glm-parent-action %s <token>", action)
	}
	if action != actionFix && len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action %s <token>", action)
	}
	if action == actionFix {
		if err := validateFixOptions(args[1:]); err != nil {
			return err
		}
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	payload, err := parentaction.Consume(repoRoot, action, args[0])
	if err != nil {
		return err
	}
	workerArgs := payloadWorkerArgs(action, payload, args[1:])
	return runResolvedWorker(worker, repoRoot, workerArgs, bytes.NewReader(payload), stdout, stderr, nil)
}

func payloadWorkerArgs(action string, payload []byte, options []string) []string {
	digest := sha256.Sum256(payload)
	mode := "--decision-stdin"
	if action == actionFix {
		mode = "--fix-stdin"
	}
	args := []string{mode, strconv.Itoa(len(payload)), "--sha256", hex.EncodeToString(digest[:])}
	return append(args, options...)
}

func directWorkerArgs(action string) []string {
	switch action {
	case actionStart:
		return []string{activeTaskRequest}
	case "accept":
		return []string{"--accept"}
	default:
		return []string{"--resume"}
	}
}

func validateFixOptions(options []string) error {
	fixUsage := "usage: glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff] [--approval-only]"
	pairs, approvalOnly, err := extractApprovalOnlyOption(options, fixUsage)
	if err != nil {
		return err
	}
	if len(pairs)%2 != 0 {
		return fmt.Errorf("%s", fixUsage)
	}
	seen := map[string]bool{}
	for index := 0; index < len(pairs); index += 2 {
		name := pairs[index]
		value := pairs[index+1]
		if seen[name] {
			return fmt.Errorf("%s", fixUsage)
		}
		seen[name] = true
		switch name {
		case "--origin":
			if !state.ValidParentOrigin(value) {
				return fmt.Errorf("%s", fixUsage)
			}
		case "--accepted-scope":
			if value != "current-diff" {
				return fmt.Errorf("%s", fixUsage)
			}
		default:
			return fmt.Errorf("%s", fixUsage)
		}
	}
	if approvalOnly && (!seen["--accepted-scope"] || seen["--origin"]) {
		return fmt.Errorf("%s", fixUsage)
	}
	return nil
}

func extractApprovalOnlyOption(options []string, usage string) ([]string, bool, error) {
	index := -1
	for current, option := range options {
		if option != "--approval-only" {
			continue
		}
		if index >= 0 {
			return nil, false, fmt.Errorf("%s", usage)
		}
		index = current
	}
	if index < 0 {
		return options, false, nil
	}
	pairs := make([]string, 0, len(options)-1)
	pairs = append(pairs, options[:index]...)
	pairs = append(pairs, options[index+1:]...)
	return pairs, true, nil
}

func runWorker(repoRoot string, args []string, stdin io.Reader, stdout, stderr io.Writer, extraEnv []string) error {
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	return runResolvedWorker(worker, repoRoot, args, stdin, stdout, stderr, extraEnv)
}

func runResolvedWorker(worker, workingDir string, args []string, stdin io.Reader, stdout, stderr io.Writer, extraEnv []string) error {
	command := exec.Command(worker, args...)
	command.Dir = workingDir
	command.Env = append(os.Environ(), extraEnv...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go forwardSignals(command.Process, signals, done)
	err := command.Wait()
	close(done)
	return err
}

func forwardSignals(process *os.Process, signals <-chan os.Signal, done <-chan struct{}) {
	for {
		select {
		case received := <-signals:
			_ = process.Signal(received)
		case <-done:
			return
		}
	}
}

func childExitCode(exitErr *exec.ExitError) int {
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
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
