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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	usage = "usage: glm-parent-action authority <rules|plan|active> | start | prepare <decision|fix|start-milestones|revise-milestones> | decision <token> | fix <token> [--origin <origin>] [--accepted-scope current-diff] [--approval-only] | start-milestones <token> | revise-milestones <token> | no-go | accept | resume | finalize-check <go-test|go-test-race>"

	activeTaskRequest = "現在のACTIVE taskを実行してください。"
	actionStart       = "start"
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
	if args[0] == "authority" {
		return authoritybootstrapcmd.Execute(args[1:], stdout)
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
		return fmt.Errorf("usage: glm-parent-action prepare <decision|fix|start-milestones|revise-milestones>")
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
	if descriptor, ok := parentaction.LookupPayloadAction(action); ok {
		extraEnv := []string(nil)
		if descriptor.Action == parentaction.ActionStartMilestones {
			extraEnv = startIdentityEnv(actionStart)
		} else if err := persistParentCodexIdentity(cfg); err != nil {
			return err
		}
		return executePayloadAction(cfg.RepoRoot, descriptor, args[1:], stdout, stderr, extraEnv)
	}

	switch action {
	case "no-go":
		return executeNoGo(cfg, args, stdout)
	case actionStart, "accept", "resume":
		return executeDirectWorkerAction(cfg, action, args, stdout, stderr)
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

func executeDirectWorkerAction(cfg config.AppConfig, action string, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action %s", action)
	}
	if action == "resume" {
		if err := persistParentCodexIdentity(cfg); err != nil {
			return err
		}
	}
	return runWorker(cfg.RepoRoot, directWorkerArgs(action), nil, stdout, stderr, startIdentityEnv(action))
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

func executePayloadAction(
	repoRoot string,
	descriptor parentaction.PayloadAction,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	extraEnv []string,
) error {
	action := string(descriptor.Action)
	if len(args) < 1 {
		return fmt.Errorf("usage: glm-parent-action %s <token>", action)
	}
	if descriptor.Action != parentaction.ActionFix && len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action %s <token>", action)
	}
	if descriptor.Action == parentaction.ActionFix {
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
	workerArgs := payloadWorkerArgsForDescriptor(descriptor, payload, args[1:])
	return runResolvedWorker(worker, repoRoot, workerArgs, bytes.NewReader(payload), stdout, stderr, extraEnv)
}

func payloadWorkerArgsForDescriptor(descriptor parentaction.PayloadAction, payload []byte, options []string) []string {
	digest := sha256.Sum256(payload)
	args := []string{descriptor.WorkerMode, strconv.Itoa(len(payload)), "--sha256", hex.EncodeToString(digest[:])}
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
	_, remaining, err := parentfix.Extract(options)
	if err != nil || len(remaining) != 0 {
		return fmt.Errorf("%s", fixUsage)
	}
	return nil
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
