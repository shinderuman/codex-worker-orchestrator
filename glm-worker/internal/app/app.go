package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type CommandMode int

type Command struct {
	Mode    CommandMode
	Payload string

	WatchVerbose bool

	StdinBytes int64

	SHA256 string

	Origin        string
	AcceptedScope string
	Role          string
	Verify        VerifyArgs
	Coalesce      CoalesceArgs
}

type VerifyArgs struct {
	Key      string
	RFC3339  string
	ThreadID string
}

type CoalesceArgs struct {
	ParentThreadID  string
	ResumeAtRFC3339 string
}

type UsageError struct {
	Message string
}

type NotFoundError struct {
	Message string
}

type StdinPayloadError struct {
	Message string
}

type stdinReadyControlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

type RunnerFactory func(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner

type commandParser func([]string) (Command, error)

const (
	ModeNewTask CommandMode = iota
	ModeDecision
	ModeFix
	ModeAccept
	ModeResume
	ModeStop
	ModeIsolate
	ModeStatus
	ModeHandoff
	ModeWatch
	ModeTimeline
	ModeConvergence
	ModeStats
	ModeReset
	ModeVerifyAutoResume
	ModeCheckWakeCoalesce
	ModeEvalAB
	ModeCallOutliers
	ModeCodexLimit
	ModeInstallSmoke
	ModeQualityGate
	ModeModelRouting
	ModeTestImpact
	ModeBundle
	ModeRepoSearch
)

const fixOriginUsage = "[--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair] [--accepted-scope current-diff]"

const installSmokeUsage = "[--role worker|reviewer|fix|parent]"

const qualityGateUsage = "<go-test|go-test-race> | --quality-gate <status|watch|result> <validation-run-id>"

var commandParsers = map[string]commandParser{
	"--decision": func([]string) (Command, error) {
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	},
	"--fix": func([]string) (Command, error) {
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	},
	"--decision-stdin": func(args []string) (Command, error) {
		return stdinPayloadCommand(ModeDecision, args, "usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>]", false)
	},
	"--fix-stdin": func(args []string) (Command, error) {
		return stdinPayloadCommand(ModeFix, args, fmt.Sprintf("usage: glm-worker --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage), true)
	},
	"--accept": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeAccept, "usage: glm-worker --accept")
	},
	"--resume": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeResume, "usage: glm-worker --resume")
	},
	"--stop": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStop, "usage: glm-worker --stop")
	},
	"--isolate": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeIsolate, "usage: glm-worker --isolate")
	},
	"--status": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStatus, "usage: glm-worker --status")
	},
	"--handoff": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeHandoff, "usage: glm-worker --handoff")
	},
	"--watch": watchCommand,
	"--timeline": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeTimeline, "usage: glm-worker --timeline [task-id]")
	},
	"--convergence": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeConvergence, "usage: glm-worker --convergence [task-id]")
	},
	"--stats": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStats, "usage: glm-worker --stats")
	},
	"--reset": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeReset, "usage: glm-worker --reset")
	},
	"--verify-auto-resume":  verifyAutoResumeCommand,
	"--check-wake-coalesce": checkWakeCoalesceCommand,
	"--eval-ab": func(args []string) (Command, error) {
		return requiredPayloadCommand(args, ModeEvalAB, "usage: glm-worker --eval-ab <run-dir>")
	},
	"--call-outliers": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeCallOutliers, "usage: glm-worker --call-outliers")
	},
	"--model-routing": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeModelRouting, "usage: glm-worker --model-routing")
	},
	"--test-impact": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeTestImpact, "usage: glm-worker --test-impact")
	},
	"--codex-limit": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeCodexLimit, "usage: glm-worker --codex-limit")
	},
	"--repo-search": func(args []string) (Command, error) {
		return requiredPayloadCommand(args, ModeRepoSearch, "usage: glm-worker --repo-search <query>")
	},
	"--install-smoke": installSmokeCommand,
	"--quality-gate":  qualityGateRecoveryCommand,
	"bundle": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeBundle, "usage: glm-worker bundle [task-id]")
	},
}

func (e *UsageError) Error() string {
	return e.Message
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func usageError(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, usageError("usage: glm-worker <instruction> | --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s | --accept | --resume | --stop | --isolate | --status | --handoff | --watch [--verbose] | --timeline [task-id] | --convergence [task-id] | --stats | --reset | --eval-ab <run-dir> | --call-outliers | --codex-limit | --repo-search <query> | --check-wake-coalesce <parent-thread-id> <auto-resume-at-rfc3339> | --install-smoke %s | --quality-gate %s | --model-routing | bundle [task-id]", fixOriginUsage, installSmokeUsage, qualityGateUsage)
	}
	if parser, ok := commandParsers[args[0]]; ok {
		return parser(args)
	}
	return Command{Mode: ModeNewTask, Payload: strings.Join(args, " ")}, nil
}

func singleArgCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 1 {
		return Command{}, usageError("%s", usage)
	}
	return Command{Mode: mode}, nil
}

func optionalPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) > 2 {
		return Command{}, usageError("%s", usage)
	}
	command := Command{Mode: mode}
	if len(args) == 2 {
		command.Payload = args[1]
	}
	return command, nil
}

func requiredPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 2 {
		return Command{}, usageError("%s", usage)
	}
	return Command{Mode: mode, Payload: args[1]}, nil
}

func watchCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeWatch}, nil
	}
	if len(args) == 2 && args[1] == "--verbose" {
		return Command{Mode: ModeWatch, WatchVerbose: true}, nil
	}
	return Command{}, usageError("usage: glm-worker --watch [--verbose]")
}

func verifyAutoResumeCommand(args []string) (Command, error) {
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
}

func checkWakeCoalesceCommand(args []string) (Command, error) {
	if len(args) != 3 {
		return Command{}, usageError("usage: glm-worker --check-wake-coalesce <parent-thread-id> <auto-resume-at-rfc3339>")
	}
	return Command{
		Mode: ModeCheckWakeCoalesce,
		Coalesce: CoalesceArgs{
			ParentThreadID:  args[1],
			ResumeAtRFC3339: args[2],
		},
	}, nil
}

func installSmokeCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeInstallSmoke}, nil
	}
	if len(args) == 3 && args[1] == "--role" && validInstallSmokeRoles[args[2]] {
		return Command{Mode: ModeInstallSmoke, Role: args[2]}, nil
	}
	return Command{}, usageError("usage: glm-worker --install-smoke %s", installSmokeUsage)
}

func stdinPayloadCommand(mode CommandMode, args []string, usage string, allowOrigin bool) (Command, error) {
	if len(args) < 2 {
		return Command{}, usageError("%s", usage)
	}
	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 {
		return Command{}, usageError("%s", usage)
	}
	options := args[2:]
	if len(options)%2 != 0 {
		return Command{}, usageError("%s", usage)
	}
	command := Command{Mode: mode, StdinBytes: payloadBytes}
	seenSHA256 := false
	for index := 0; index < len(options); index += 2 {
		if err := applyStdinPayloadOption(&command, options[index], options[index+1], usage, allowOrigin, &seenSHA256); err != nil {
			return Command{}, err
		}
	}
	return command, nil
}

func applyStdinPayloadOption(command *Command, name, value, usage string, allowOrigin bool, seenSHA256 *bool) error {
	if name == "--accepted-scope" {
		return applyAcceptedScopeOption(command, value, usage, allowOrigin)
	}
	switch name {
	case "--sha256":
		if *seenSHA256 {
			return usageError("%s", usage)
		}
		digest, err := parsePayloadSHA256(value)
		if err != nil {
			return usageError("%s", usage)
		}
		command.SHA256 = digest
		*seenSHA256 = true
		return nil
	case "--origin":
		if !allowOrigin || command.Origin != "" || !state.ValidParentOrigin(value) {
			return usageError("%s", usage)
		}
		command.Origin = value
		return nil
	default:
		return usageError("%s", usage)
	}
}

func applyAcceptedScopeOption(command *Command, value, usage string, allowOrigin bool) error {
	if !allowOrigin || command.AcceptedScope != "" || value != "current-diff" {
		return usageError("%s", usage)
	}
	command.AcceptedScope = value
	return nil
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

func defaultRunnerFactory(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner {
	r := runner.NewClaudeRunner(cfg, st)
	r.AttachStopController(stop)
	return r
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
	return dispatchMachineOutput(cmd, cfg, runnerFactory, stdout, stderr)
}

func Execute(cmd Command, cfg config.AppConfig, rf RunnerFactory, stdout, _ io.Writer) error {
	if cmd.StdinBytes > 0 && cmd.Payload == "" {
		return fmt.Errorf("stdin payload mode requires the payload to be read before execute")
	}
	if handled, err := executeStateless(cmd, cfg, stdout); handled {
		return err
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if handled, err := executeStateOnly(cmd, cfg, st, stdout); handled {
		return err
	}

	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := admitParentCommand(cmd, st); err != nil {
		return err
	}

	if handled, err := executeLocked(cmd, cfg, st, stdout); handled {
		return err
	}
	return executeWorkflow(cmd, cfg, st, rf, stdout)
}

func executeStateless(cmd Command, cfg config.AppConfig, stdout io.Writer) (bool, error) {
	switch cmd.Mode {
	case ModeWatch:
		return true, printWatch(state.AttachStateStore(cfg), stdout, defaultWatchOptions(cmd.WatchVerbose))
	case ModeStop:
		return true, requestStop(cfg, stdout)
	case ModeCodexLimit:
		return true, printCodexLimit(cfg, stdout)
	case ModeRepoSearch:
		return true, printRepoSearch(cmd.Payload, cfg, stdout)
	case ModeCheckWakeCoalesce:
		return true, printCheckWakeCoalesce(cmd, cfg, stdout)
	default:
		return executeStatelessReport(cmd, cfg, stdout)
	}
}

func executeStatelessReport(cmd Command, cfg config.AppConfig, stdout io.Writer) (bool, error) {
	st := state.AttachStateStore(cfg)
	switch cmd.Mode {
	case ModeTimeline:
		return true, printTimeline(st, cmd.Payload, stdout)
	case ModeConvergence:
		return true, printConvergence(st, cmd.Payload, stdout)
	case ModeEvalAB:
		return true, printEvalAB(st, cmd.Payload, stdout)
	case ModeCallOutliers:
		return true, printCallOutliers(st, stdout)
	case ModeModelRouting:
		return true, printModelRouting(st, stdout)
	case ModeTestImpact:
		return true, printTestImpact(st, stdout)
	case ModeBundle:
		return true, printBundle(cfg, st, cmd.Payload, stdout)
	default:
		return false, nil
	}
}

func executeStateOnly(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) (bool, error) {
	switch cmd.Mode {
	case ModeStatus:
		return true, printStatus(st, stdout)
	case ModeHandoff:
		return true, printParentHandoff(st, stdout)
	case ModeStats:
		return true, printStats(st, stdout)
	case ModeVerifyAutoResume:
		return true, printVerifyAutoResume(cmd, cfg, stdout)
	case ModeInstallSmoke:
		return true, runInstallSmoke(cmd.Role, cfg, st, stdout)
	case ModeQualityGate:
		return true, runQualityGate(cmd.Payload, st, stdout)
	default:
		return false, nil
	}
}

func executeLocked(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) (bool, error) {
	switch cmd.Mode {
	case ModeReset:
		return true, resetState(st, stdout)
	case ModeAccept:
		return true, parentAccept(st, stdout)
	case ModeIsolate:
		return true, isolateInterruptedTask(st, cfg, stdout)
	case modeRotateInstructionBaseline:
		return true, rotateInstructionBaseline(cfg, st, stdout)
	default:
		return false, nil
	}
}

func executeWorkflow(cmd Command, cfg config.AppConfig, st *state.StateStore, rf RunnerFactory, stdout io.Writer) error {
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
		return wf.ExecuteExplicitFixWithScope(cmd.Payload, cmd.Origin, cmd.AcceptedScope)
	case ModeResume:
		return wf.ExecuteResume()
	default:
		return fmt.Errorf("unsupported command mode")
	}
}
