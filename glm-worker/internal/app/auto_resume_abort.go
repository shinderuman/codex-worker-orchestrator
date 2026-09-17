package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type autoResumeParentOutput struct {
	autoresume.AutoResumeOutput
	FallbackCommand []string `json:"fallback_command,omitempty"`
}

const autoResumeFallbackUsage = "usage: glm-worker --auto-resume-fallback <transaction-token>"

var autoResumeFallbackWaitUntil = func(target time.Time) {
	if delay := time.Until(target); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
	}
}

var autoResumeFallbackRunResume = runAutoResumeFallbackParentAction

func init() {
	commandParsers["--auto-resume-fallback"] = autoResumeFallbackCommand
}

func autoResumeOutputForParent(output autoresume.AutoResumeOutput) any {
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Token == "" {
		return output
	}
	return autoResumeParentOutput{
		AutoResumeOutput: output,
		FallbackCommand: []string{
			"glm-worker",
			"--auto-resume-fallback",
			output.Token,
		},
	}
}

func autoResumeFallbackCommand(args []string) (Command, error) {
	if len(args) != 2 || args[1] == "" {
		return Command{}, machinecli.UsageErrorf("%s", autoResumeFallbackUsage)
	}
	return Command{
		Mode:       ModeAutoResumeResponse,
		Payload:    autoresume.AutoResumeAbortAutomationUpdateUnavailable,
		AutoResume: AutoResumeArgs{Token: args[1]},
	}, nil
}

// printAutoResumeAbort is retained as the internal dispatch hook used by
// printAutoResumeResponse. The automation-unavailable path now performs the
// bounded fallback instead of aborting the logical task.
func printAutoResumeAbort(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	return printAutoResumeFallback(cmd, cfg, stdout)
}

func printAutoResumeFallback(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
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

	plan, err := autoresume.AutoResumeFallbackPlanFromToken(cmd.AutoResume.Token)
	if err != nil {
		return err
	}
	if err := requireAutoResumeParentThread(plan.ParentThreadID); err != nil {
		return err
	}
	if err := validateAutoResumeFallbackState(cfg, plan); err != nil {
		return err
	}
	resumeAt, err := time.Parse(time.RFC3339, plan.ResumeAtRFC3339)
	if err != nil {
		return fmt.Errorf("auto-resume fallback time is invalid: %w", err)
	}

	autoResumeFallbackWaitUntil(resumeAt)

	if err := validateAutoResumeFallbackState(cfg, plan); err != nil {
		if markErr := lease.markDelivering(); markErr != nil {
			return errors.Join(err, markErr)
		}
		delivering = true
		retryable = false
		if commitErr := lease.commit(); commitErr != nil {
			return errors.Join(err, commitErr)
		}
		return err
	}

	if err := lease.markDelivering(); err != nil {
		return err
	}
	delivering = true
	started, resumeErr := autoResumeFallbackRunResume(cfg, stdout)
	if !started {
		return resumeErr
	}
	retryable = false
	commitErr := lease.commit()
	if resumeErr != nil || commitErr != nil {
		return errors.Join(resumeErr, commitErr)
	}
	return nil
}

func validateAutoResumeFallbackState(cfg config.AppConfig, plan autoresume.AutoResumeFallbackPlan) error {
	if filepath.Clean(plan.RepoRoot) != filepath.Clean(cfg.RepoRoot) {
		return fmt.Errorf("auto-resume fallback repository changed: got %q want %q", cfg.RepoRoot, plan.RepoRoot)
	}
	st := state.AttachStateStore(cfg)
	if st.TaskStatus() != state.TaskStatusRateLimited {
		return fmt.Errorf("auto-resume fallback task is no longer rate-limited")
	}
	taskID, err := st.TaskID()
	if err != nil {
		return fmt.Errorf("auto-resume fallback task identity is unavailable: %w", err)
	}
	if taskID != plan.TaskID {
		return fmt.Errorf("auto-resume fallback task changed: got %q want %q", taskID, plan.TaskID)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return fmt.Errorf("auto-resume fallback checkpoint is unavailable: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopRateLimited {
		return fmt.Errorf("auto-resume fallback checkpoint is not rate-limited")
	}
	if checkpoint.ResetAtRFC3339 != plan.ResetAtRFC3339 {
		return fmt.Errorf("auto-resume fallback reset boundary changed: got %q want %q", checkpoint.ResetAtRFC3339, plan.ResetAtRFC3339)
	}
	available, resumeAt := runner.AutoResumeAtFromReset(checkpoint.ResetAtRFC3339)
	if !available || resumeAt != plan.ResumeAtRFC3339 {
		return fmt.Errorf("auto-resume fallback resume boundary changed: got %q want %q", resumeAt, plan.ResumeAtRFC3339)
	}
	return nil
}

func runAutoResumeFallbackParentAction(cfg config.AppConfig, stdout io.Writer) (bool, error) {
	parentAction, err := resolveGLMParentAction()
	if err != nil {
		return false, err
	}
	command := exec.Command(parentAction, "resume")
	command.Dir = cfg.RepoRoot
	command.Env = os.Environ()
	command.Stdout = stdout
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start glm-parent-action resume: %w", err)
	}
	if err := command.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return true, fmt.Errorf("glm-parent-action resume failed: %w", err)
		}
		return true, fmt.Errorf("glm-parent-action resume failed: %w: %s", err, detail)
	}
	return true, nil
}

func resolveGLMParentAction() (string, error) {
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "glm-parent-action")
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	if candidate, err := exec.LookPath("glm-parent-action"); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("glm-parent-action executable is unavailable")
}
