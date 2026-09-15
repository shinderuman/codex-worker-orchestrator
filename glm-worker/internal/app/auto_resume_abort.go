package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
)

type autoResumeParentOutput struct {
	autoresume.AutoResumeOutput
	AbortCommand []string `json:"abort_command,omitempty"`
}

const autoResumeAbortUsage = "usage: glm-worker --auto-resume-abort <transaction-token> automation_update_unavailable"

func init() {
	commandParsers["--auto-resume-abort"] = autoResumeAbortCommand
}

func autoResumeOutputForParent(output autoresume.AutoResumeOutput) any {
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Token == "" {
		return output
	}
	return autoResumeParentOutput{
		AutoResumeOutput: output,
		AbortCommand: []string{
			"glm-worker",
			"--auto-resume-abort",
			output.Token,
			autoresume.AutoResumeAbortAutomationUpdateUnavailable,
		},
	}
}

func autoResumeAbortCommand(args []string) (Command, error) {
	if len(args) != 3 || args[1] == "" || args[2] != autoresume.AutoResumeAbortAutomationUpdateUnavailable {
		return Command{}, machinecli.UsageErrorf("%s", autoResumeAbortUsage)
	}
	return Command{
		Mode:       ModeAutoResumeResponse,
		Payload:    args[2],
		AutoResume: AutoResumeArgs{Token: args[1]},
	}, nil
}

func printAutoResumeAbort(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, cmd.AutoResume.Token)
	if err != nil {
		return err
	}
	retryable := true
	delivering := false
	defer func() {
		if !retryable {
			return
		}
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
	output := autoresume.AbortAutoResumeTransaction(cmd.AutoResume.Token, cmd.Payload)
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
