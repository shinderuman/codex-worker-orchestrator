package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type CommandMode int

const (
	ModeNewTask CommandMode = iota
	ModeDecision
	ModeFix
	ModeAccept
	ModeResume
	ModeStop
	ModeIsolate
	ModeStatus
	ModeWatch
	ModeTimeline
	ModeConvergence
	ModeStats
	ModeReset
	ModeVerifyAutoResume
	ModeEvalAB
	ModeCallOutliers
	ModeCodexLimit
)

type Command struct {
	Mode    CommandMode
	Payload string

	WatchVerbose bool

	StdinBytes int64

	SHA256 string

	Origin string
	Verify VerifyArgs
}

type VerifyArgs struct {
	Key      string
	RFC3339  string
	ThreadID string
}

const fixOriginUsage = "[--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair]"

type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func usageError(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, usageError("usage: glm-worker <instruction> | --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s | --accept | --resume | --stop | --isolate | --status | --watch [--verbose] | --timeline [task-id] | --convergence [task-id] | --stats | --reset | --eval-ab <run-dir> | --call-outliers | --codex-limit", fixOriginUsage)
	}

	switch args[0] {

	case "--decision", "--fix":
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	case "--decision-stdin":
		return stdinPayloadCommand(ModeDecision, args, "usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>]", false)
	case "--fix-stdin":
		return stdinPayloadCommand(ModeFix, args, fmt.Sprintf("usage: glm-worker --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage), true)
	case "--accept":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --accept")
		}
		return Command{Mode: ModeAccept}, nil
	case "--resume":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --resume")
		}
		return Command{Mode: ModeResume}, nil
	case "--stop":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --stop")
		}
		return Command{Mode: ModeStop}, nil
	case "--isolate":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --isolate")
		}
		return Command{Mode: ModeIsolate}, nil
	case "--status":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --status")
		}
		return Command{Mode: ModeStatus}, nil
	case "--watch":
		if len(args) == 2 && args[1] == "--verbose" {
			return Command{Mode: ModeWatch, WatchVerbose: true}, nil
		}
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --watch [--verbose]")
		}
		return Command{Mode: ModeWatch}, nil
	case "--timeline":
		if len(args) > 2 {
			return Command{}, usageError("usage: glm-worker --timeline [task-id]")
		}
		if len(args) == 2 {
			return Command{Mode: ModeTimeline, Payload: args[1]}, nil
		}
		return Command{Mode: ModeTimeline}, nil
	case "--convergence":
		if len(args) > 2 {
			return Command{}, usageError("usage: glm-worker --convergence [task-id]")
		}
		if len(args) == 2 {
			return Command{Mode: ModeConvergence, Payload: args[1]}, nil
		}
		return Command{Mode: ModeConvergence}, nil
	case "--stats":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --stats")
		}
		return Command{Mode: ModeStats}, nil
	case "--reset":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --reset")
		}
		return Command{Mode: ModeReset}, nil
	case "--verify-auto-resume":
		if len(args) != 4 {
			return Command{}, usageError("usage: glm-worker --verify-auto-resume <automation-key> <auto-resume-at-rfc3339> <thread-id>")
		}
		return Command{
			Mode: ModeVerifyAutoResume,
			Verify: VerifyArgs{
				Key:      args[1],
				RFC3339:  args[2],
				ThreadID: args[3],
			},
		}, nil
	case "--eval-ab":
		if len(args) != 2 {
			return Command{}, usageError("usage: glm-worker --eval-ab <run-dir>")
		}
		return Command{Mode: ModeEvalAB, Payload: args[1]}, nil
	case "--call-outliers":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --call-outliers")
		}
		return Command{Mode: ModeCallOutliers}, nil
	case "--codex-limit":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --codex-limit")
		}
		return Command{Mode: ModeCodexLimit}, nil
	default:
		return Command{Mode: ModeNewTask, Payload: strings.Join(args, " ")}, nil
	}
}

func stdinPayloadCommand(mode CommandMode, args []string, usage string, allowOrigin bool) (Command, error) {
	if len(args) < 2 {
		return Command{}, usageError("%s", usage)
	}

	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 {
		return Command{}, usageError("%s", usage)
	}

	command := Command{Mode: mode, StdinBytes: payloadBytes}
	options := args[2:]
	if len(options)%2 != 0 {
		return Command{}, usageError("%s", usage)
	}
	seenSHA256 := false
	for index := 0; index < len(options); index += 2 {
		switch options[index] {
		case "--sha256":
			if seenSHA256 {
				return Command{}, usageError("%s", usage)
			}
			digest, err := parsePayloadSHA256(options[index+1])
			if err != nil {
				return Command{}, usageError("%s", usage)
			}
			command.SHA256 = digest
			seenSHA256 = true
		case "--origin":
			if !allowOrigin || command.Origin != "" {
				return Command{}, usageError("%s", usage)
			}
			if !state.ValidParentOrigin(options[index+1]) {
				return Command{}, usageError("%s", usage)
			}
			command.Origin = options[index+1]
		default:
			return Command{}, usageError("%s", usage)
		}
	}
	return command, nil
}

func parsePayloadSHA256(value string) (string, error) {
	if len(value) != 64 {
		return "", fmt.Errorf("sha256 must be 64 hex characters")
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return "", fmt.Errorf("sha256 must be 64 hex characters")
		}
	}
	return strings.ToLower(value), nil
}

type StdinPayloadError struct {
	Message string
}

func (e *StdinPayloadError) Error() string {
	return e.Message
}

func readStdinPayload(in io.Reader, want int64, expectedSHA string) (string, error) {
	var buf bytes.Buffer
	written, err := io.CopyN(&buf, in, want)
	if err != nil {
		return "", &StdinPayloadError{Message: fmt.Sprintf("stdin payload read failed after %d of %d bytes: %v", written, want, err)}
	}

	payload := buf.Bytes()
	if expectedSHA != "" {
		sum := sha256.Sum256(payload)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, expectedSHA) {
			return "", &StdinPayloadError{Message: fmt.Sprintf("stdin payload sha256 mismatch: expected %s, got %s", expectedSHA, actual)}
		}
	}
	return string(payload), nil
}

type stdinReadyControlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

func emitStdinReadyControlEvent(w io.Writer) error {
	line, err := marshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		return fmt.Errorf("stdin ready control event encode failed: %w", err)
	}
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("stdin ready control event write failed: %w", err)
	}
	return nil
}

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

func defaultRunnerFactory(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner {
	r := runner.NewClaudeRunner(cfg, st)
	r.AttachStopController(stop)
	return r
}

func Run(args []string) error {
	return run(args, config.Load, defaultRunnerFactory, os.Stdin, os.Stdout, os.Stderr)
}

func run(
	args []string,
	loadConfig func() (config.AppConfig, error),
	runnerFactory RunnerFactory,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	cmd, err := ParseCommand(args)
	if err != nil {
		return err
	}
	if cmd.StdinBytes > 0 {
		restore, rawApplied, err := enterStdinRawMode(stdin)
		if err != nil {
			return err
		}
		if rawApplied {
			if markerErr := emitStdinReadyControlEvent(stderr); markerErr != nil {
				return errors.Join(markerErr, restore())
			}
		}
		payload, readErr := readStdinPayload(stdin, cmd.StdinBytes, cmd.SHA256)
		if err := errors.Join(readErr, restore()); err != nil {
			return err
		}
		cmd.Payload = payload
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return Execute(cmd, cfg, runnerFactory, stdout, stderr)
}

func Execute(cmd Command, cfg config.AppConfig, rf RunnerFactory, stdout, stderr io.Writer) error {
	if cmd.StdinBytes > 0 && cmd.Payload == "" {
		return fmt.Errorf("stdin payload mode requires the payload to be read before execute")
	}
	if cmd.Mode == ModeWatch {
		return printWatch(state.AttachStateStore(cfg), stdout, defaultWatchOptions(cmd.WatchVerbose))
	}
	if cmd.Mode == ModeTimeline {
		return printTimeline(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}
	if cmd.Mode == ModeConvergence {
		return printConvergence(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}
	if cmd.Mode == ModeEvalAB {
		return printEvalAB(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}
	if cmd.Mode == ModeCallOutliers {
		return printCallOutliers(state.AttachStateStore(cfg), stdout)
	}
	if cmd.Mode == ModeStop {
		return requestStop(cfg, stdout)
	}

	if cmd.Mode == ModeCodexLimit {
		return printCodexLimit(cfg, stdout)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}

	switch cmd.Mode {
	case ModeStatus:
		return printStatus(st, stdout)
	case ModeStats:
		return printStats(st, stdout)
	case ModeVerifyAutoResume:
		return printVerifyAutoResume(cmd, cfg, stdout)
	}

	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer lock.Close()

	if cmd.Mode == ModeReset {
		return resetState(st, stdout)
	}

	if cmd.Mode == ModeAccept {
		return parentAccept(st, stdout)
	}

	if cmd.Mode == ModeIsolate {
		return isolateInterruptedTask(st, cfg, stdout)
	}

	controller := runner.NewStopController()
	stopServer, err := startStopEndpoint(st, controller)
	if err != nil {
		return err
	}
	defer stopServer.Close()

	r := rf(cfg, st, controller)
	wf := workflow.NewWorkflow(cfg, st, r, stdout)
	wf.AttachStopController(controller)

	switch cmd.Mode {
	case ModeNewTask:
		return wf.ExecuteNewTask(cmd.Payload)
	case ModeDecision:
		return wf.ExecuteDecision(cmd.Payload)
	case ModeFix:
		return wf.ExecuteExplicitFix(cmd.Payload, cmd.Origin)
	case ModeResume:
		return wf.ExecuteResume()
	default:
		return fmt.Errorf("unsupported command mode")
	}
}
