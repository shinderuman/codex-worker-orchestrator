package app

import (
	"io"
	"os"
	"strconv"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type CodexWakeArgs struct {
	ThreadID     string
	AutomationID string
	Token        string
}

const (
	codexWakePlanUsage     = "usage: glm-worker --codex-wake-plan <wake-task-thread-id> [--fired-automation-id <automation-id>]"
	codexWakeResponseUsage = "usage: glm-worker --codex-wake-response-stdin <payload-bytes> <transaction-token> [--sha256 <hex>]"
)

var codexWakeCommandParsers = map[string]commandParser{
	"--codex-wake-plan":           codexWakePlanCommand,
	"--codex-wake-response-stdin": codexWakeResponseCommand,
}

func codexWakePlanCommand(args []string) (Command, error) {
	if len(args) != 2 && len(args) != 4 {
		return Command{}, usageError("%s", codexWakePlanUsage)
	}
	if !state.ValidUUIDFormat(args[1]) {
		return Command{}, usageError("%s", codexWakePlanUsage)
	}
	command := Command{Mode: ModeCodexWakePlan, CodexWake: CodexWakeArgs{ThreadID: args[1]}}
	if len(args) == 4 {
		if args[2] != "--fired-automation-id" || args[3] == "" {
			return Command{}, usageError("%s", codexWakePlanUsage)
		}
		command.CodexWake.AutomationID = args[3]
	}
	return command, nil
}

func codexWakeResponseCommand(args []string) (Command, error) {
	if len(args) != 3 && len(args) != 5 {
		return Command{}, usageError("%s", codexWakeResponseUsage)
	}
	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 || args[2] == "" {
		return Command{}, usageError("%s", codexWakeResponseUsage)
	}
	command := Command{
		Mode:       ModeCodexWakeResponse,
		StdinBytes: payloadBytes,
		CodexWake:  CodexWakeArgs{Token: args[2]},
	}
	if len(args) == 5 {
		seenSHA256 := false
		if err := applyStdinPayloadOption(&command, args[3], args[4], codexWakeResponseUsage, &seenSHA256); err != nil {
			return Command{}, err
		}
	}
	return command, nil
}

func executeCodexWakeStateless(cmd Command, cfg config.AppConfig, stdout io.Writer) (bool, error) {
	switch cmd.Mode {
	case ModeCodexWakePlan:
		return true, printCodexWakePlan(cmd, cfg, stdout)
	case ModeCodexWakeResponse:
		return true, printCodexWakeResponse(cmd, cfg, stdout)
	default:
		return executeStatelessReport(cmd, cfg, stdout)
	}
}

func printCodexWakePlan(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	if cmd.CodexWake.AutomationID != "" {
		if err := requireCodexWakeInvocationThread(cmd.CodexWake.ThreadID); err != nil {
			return err
		}
	}
	snapshot, err := readCodexLimitSnapshot(cfg)
	if err != nil {
		return err
	}
	automationsDir, _ := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output, err := autoresume.BuildCodexWakeTransaction(
		snapshot,
		cmd.CodexWake.ThreadID,
		cmd.CodexWake.AutomationID,
		automationsDir,
		time.Now(),
	)
	if err != nil {
		return err
	}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, output.Token); err != nil {
		return err
	}
	if err := writeJSON(stdout, output); err != nil {
		removeCodexWakeToken(cfg.CodexConfigDir, output.Token)
		return err
	}
	return nil
}

func printCodexWakeResponse(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	lease, err := beginCodexWakeToken(cfg.CodexConfigDir, cmd.CodexWake.Token)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			lease.rollback()
		}
	}()

	wakeThreadID, wakeInvocation, err := autoresume.CodexWakeTransactionContext(cmd.CodexWake.Token)
	if err != nil {
		return err
	}
	if wakeInvocation {
		if err := requireCodexWakeInvocationThread(wakeThreadID); err != nil {
			return err
		}
	}
	automationsDir, dbPath := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output := autoresume.AdvanceCodexWakeTransaction(
		cmd.CodexWake.Token,
		[]byte(cmd.Payload),
		automationsDir,
		dbPath,
		autoresume.ReadDBRowSqlite3,
	)
	if output.Token != "" {
		if err := persistCodexWakeToken(cfg.CodexConfigDir, output.Token); err != nil {
			return err
		}
	}
	if err := writeJSON(stdout, output); err != nil {
		removeCodexWakeToken(cfg.CodexConfigDir, output.Token)
		return err
	}
	committed = true
	if err := lease.commit(); err != nil {
		return err
	}
	return nil
}

func requireCodexWakeInvocationThread(wakeThreadID string) error {
	if os.Getenv(codexThreadIDEnv) != wakeThreadID {
		return &NotFoundError{Message: codexThreadIDEnv + " does not match the wake task thread"}
	}
	return nil
}
