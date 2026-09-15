package app

import (
	"io"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type AutoResumeArgs struct {
	RunControl     string
	ParentThreadID string
	Token          string
}

const (
	autoResumePlanUsage     = "usage: glm-worker --auto-resume-plan [--run-control <text>]"
	autoResumeResponseUsage = "usage: glm-worker --auto-resume-response-stdin <payload-bytes> <transaction-token> [--sha256 <hex>]"
)

func autoResumePlanCommand(args []string) (Command, error) {
	if len(args) != 1 && len(args) != 3 {
		return Command{}, usageError("%s", autoResumePlanUsage)
	}
	command := Command{Mode: ModeAutoResumePlan}
	if len(args) == 3 {
		if args[1] != "--run-control" || args[2] == "" {
			return Command{}, usageError("%s", autoResumePlanUsage)
		}
		command.AutoResume.RunControl = args[2]
	}
	return command, nil
}

func autoResumeResponseCommand(args []string) (Command, error) {
	return transactionResponseCommand(args, ModeAutoResumeResponse, autoResumeResponseUsage, func(command *Command, token string) {
		command.AutoResume.Token = token
	})
}

func printAutoResumePlan(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	st := state.AttachStateStore(cfg)
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return &NotFoundError{Message: "rate-limited task state is not readable: " + err.Error()}
	}
	if st.TaskStatus() != state.TaskStatusRateLimited || checkpoint.StopKind != state.ResumeStopRateLimited {
		return &NotFoundError{Message: "current task state is not a rate-limited stop"}
	}
	taskID := st.ReadOr("task.id", "")
	repoRoot := st.ReadOr("repo-root", "")
	if taskID == "" || repoRoot == "" || checkpoint.ResetAtRFC3339 == "" {
		return &NotFoundError{Message: "rate-limit stop evidence is incomplete"}
	}
	available, resumeAt := runner.AutoResumeAtFromReset(checkpoint.ResetAtRFC3339)
	if !available {
		return &NotFoundError{Message: "rate-limit reset time is unavailable"}
	}
	automationsDir, dbPath := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output, err := autoresume.BuildAutoResumeTransaction(autoresume.AutoResumePlanParams{
		TaskID:          taskID,
		RepoRoot:        repoRoot,
		ParentThreadID:  cmd.AutoResume.ParentThreadID,
		AutomationKey:   runner.AutoResumeKeyFor(cfg.RepoShort, taskID),
		ResetAtRFC3339:  checkpoint.ResetAtRFC3339,
		ResumeAtRFC3339: resumeAt,
		RunControl:      cmd.AutoResume.RunControl,
		AutomationsDir:  automationsDir,
		DBPath:          dbPath,
		Now:             time.Now(),
	}, autoresume.ReadDBRowSqlite3)
	if err != nil {
		return err
	}
	if output.Token == "" {
		return writeJSON(stdout, output)
	}
	if err := persistAutoResumeToken(cfg.CodexConfigDir, output.Token); err != nil {
		return err
	}
	if err := writeJSON(stdout, output); err != nil {
		removeAutoResumeToken(cfg.CodexConfigDir, output.Token)
		return err
	}
	return nil
}

func printAutoResumeResponse(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, cmd.AutoResume.Token)
	if err != nil {
		return err
	}
	retryable := true
	delivering := false
	successorToken := ""
	defer func() {
		if !retryable {
			return
		}
		removeAutoResumeToken(cfg.CodexConfigDir, successorToken)
		if delivering {
			lease.rollbackDelivering()
			return
		}
		lease.rollback()
	}()

	parentThreadID, err := autoresume.AutoResumeTransactionContext(cmd.AutoResume.Token)
	if err != nil {
		return err
	}
	if err := requireAutoResumeParentThread(parentThreadID); err != nil {
		return err
	}
	automationsDir, dbPath := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output := autoresume.AdvanceAutoResumeTransaction(
		cmd.AutoResume.Token,
		[]byte(cmd.Payload),
		automationsDir,
		dbPath,
		autoresume.ReadDBRowSqlite3,
	)
	successorToken = output.Token
	if successorToken != "" {
		if err := persistAutoResumeToken(cfg.CodexConfigDir, successorToken); err != nil {
			return err
		}
	}
	if err := lease.markDelivering(); err != nil {
		return err
	}
	delivering = true
	complete, err := writeTransactionJSON(stdout, output)
	if !complete {
		return err
	}
	retryable = false
	return lease.commit()
}

func requireAutoResumeParentThread(parentThreadID string) error {
	if os.Getenv(codexThreadIDEnv) != parentThreadID {
		return &NotFoundError{Message: codexThreadIDEnv + " does not match the transaction parent thread"}
	}
	return nil
}
