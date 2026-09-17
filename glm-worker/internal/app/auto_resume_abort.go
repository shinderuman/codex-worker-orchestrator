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

type autoResumeFallbackExternalOutput struct {
	Status               string `json:"status"`
	TaskID               string `json:"task_id"`
	ExpectedAutomationID string `json:"expected_automation_id"`
	ResumeAtRFC3339      string `json:"resume_at_rfc3339"`
}

type autoResumeFallbackExecution struct {
	cfg            config.AppConfig
	stdout         io.Writer
	token          string
	lease          transactionTokenLease
	retryable      bool
	delivering     bool
	plan           autoresume.AutoResumeFallbackPlan
	automationsDir string
	dbPath         string
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
var autoResumeFallbackEvaluate = autoresume.EvaluateAutoResumeFallback

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

func printAutoResumeFallback(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, cmd.AutoResume.Token)
	if err != nil {
		return err
	}
	execution := autoResumeFallbackExecution{
		cfg:       cfg,
		stdout:    stdout,
		token:     cmd.AutoResume.Token,
		lease:     lease,
		retryable: true,
	}
	defer execution.rollback()
	return execution.run()
}

func (execution *autoResumeFallbackExecution) rollback() {
	if !execution.retryable {
		return
	}
	if execution.delivering {
		execution.lease.rollbackDelivering()
		return
	}
	execution.lease.rollback()
}

func (execution *autoResumeFallbackExecution) run() error {
	plan, err := autoresume.AutoResumeFallbackPlanFromToken(execution.token)
	if err != nil {
		return err
	}
	execution.plan = plan
	if err := requireAutoResumeParentThread(plan.ParentThreadID); err != nil {
		return err
	}
	if err := validateAutoResumeFallbackState(execution.cfg, plan); err != nil {
		return err
	}
	execution.automationsDir, execution.dbPath = autoresume.CodexWakePersistencePaths(execution.cfg.CodexConfigDir)
	decision, err := execution.evaluatePersistence()
	if err != nil {
		return err
	}
	if decision == autoresume.AutoResumeFallbackExternalWake {
		return execution.finishExternalWake()
	}
	if err := requireAutoResumeFallbackLocalWait(decision, false); err != nil {
		return err
	}
	resetAt, err := time.Parse(time.RFC3339, execution.plan.ResetAtRFC3339)
	if err != nil {
		return fmt.Errorf("auto-resume fallback reset time is invalid: %w", err)
	}
	autoResumeFallbackWaitUntil(resetAt)
	return execution.resumeAfterWait()
}

func (execution *autoResumeFallbackExecution) evaluatePersistence() (autoresume.AutoResumeFallbackDecision, error) {
	plan, decision, err := autoResumeFallbackEvaluate(
		execution.token,
		execution.automationsDir,
		execution.dbPath,
		autoresume.ReadDBRowSqlite3,
	)
	if err != nil {
		return "", err
	}
	execution.plan = plan
	return decision, nil
}

func (execution *autoResumeFallbackExecution) resumeAfterWait() error {
	if err := validateAutoResumeFallbackState(execution.cfg, execution.plan); err != nil {
		return execution.consumeStateFailure(err)
	}
	decision, err := execution.evaluatePersistence()
	if err != nil {
		return err
	}
	if decision == autoresume.AutoResumeFallbackExternalWake {
		return execution.finishExternalWake()
	}
	if err := requireAutoResumeFallbackLocalWait(decision, true); err != nil {
		return err
	}
	return execution.finishLocalResume()
}

func requireAutoResumeFallbackLocalWait(decision autoresume.AutoResumeFallbackDecision, afterWait bool) error {
	if decision == autoresume.AutoResumeFallbackLocalWait {
		return nil
	}
	if afterWait {
		return fmt.Errorf("auto-resume fallback decision after wait is invalid: %q", decision)
	}
	return fmt.Errorf("auto-resume fallback decision is invalid: %q", decision)
}

func (execution *autoResumeFallbackExecution) finishExternalWake() error {
	if err := execution.lease.markDelivering(); err != nil {
		return err
	}
	execution.delivering = true
	complete, writeErr := writeTransactionJSON(execution.stdout, autoResumeFallbackExternalOutput{
		Status:               "external_wake_active",
		TaskID:               execution.plan.TaskID,
		ExpectedAutomationID: execution.plan.ExpectedAutomationID,
		ResumeAtRFC3339:      execution.plan.ResumeAtRFC3339,
	})
	if !complete {
		return writeErr
	}
	execution.retryable = false
	return execution.lease.commit()
}

func (execution *autoResumeFallbackExecution) finishLocalResume() error {
	if err := execution.lease.markDelivering(); err != nil {
		return err
	}
	execution.delivering = true
	started, resumeErr := autoResumeFallbackRunResume(execution.cfg, execution.stdout)
	if !started {
		return resumeErr
	}
	execution.retryable = false
	return errors.Join(resumeErr, execution.lease.commit())
}

func (execution *autoResumeFallbackExecution) consumeStateFailure(cause error) error {
	consumed, err := consumeAutoResumeFallbackFailure(execution.lease, cause)
	if consumed {
		execution.delivering = true
		execution.retryable = false
	}
	return err
}

func consumeAutoResumeFallbackFailure(lease transactionTokenLease, cause error) (bool, error) {
	if err := lease.markDelivering(); err != nil {
		return false, errors.Join(cause, err)
	}
	if err := lease.commit(); err != nil {
		return true, errors.Join(cause, err)
	}
	return true, cause
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
