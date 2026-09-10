package parentactioncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type installOutput struct {
	Status  string               `json:"status"`
	Failure *finalizationFailure `json:"failure,omitempty"`
}

const (
	installStatusInstalled     = "installed"
	installStatusFailed        = "install_failed"
	installStatusGuardRejected = "install_guard_rejected"
	installScriptName          = "install.sh"
)

func executeInstall(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action install")
	}
	st := state.AttachStateStore(cfg)
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	if !plan.Allows(state.ParentActionInstall) {
		return fmt.Errorf("install is not admitted for the current task (required action %s)", plan.RequiredAction)
	}
	if failure := installRepositoryGuard(cfg.RepoRoot); failure != nil {
		return json.NewEncoder(stdout).Encode(installOutput{Status: installStatusGuardRejected, Failure: failure})
	}
	script, failure := installScriptGuard(cfg.RepoRoot)
	if failure != nil {
		return json.NewEncoder(stdout).Encode(installOutput{Status: installStatusGuardRejected, Failure: failure})
	}
	return runInstallScript(script, cfg.RepoRoot, stdout, stderr)
}

func installRepositoryGuard(repoRoot string) *finalizationFailure {
	decision, err := repositoryharness.Evaluate(repoRoot)
	if err != nil {
		return &finalizationFailure{Stage: "install", Reason: "repository_harness_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if !decision.Active {
		return &finalizationFailure{Stage: "install", Reason: "repository_harness_inactive", Detail: decision.Reason}
	}
	return nil
}

func installScriptGuard(repoRoot string) (string, *finalizationFailure) {
	script := filepath.Join(repoRoot, installScriptName)
	info, err := os.Lstat(script)
	if err != nil {
		return "", &finalizationFailure{Stage: "install", Reason: "install_script_missing"}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", &finalizationFailure{Stage: "install", Reason: "install_script_symlink"}
	}
	if !info.Mode().IsRegular() {
		return "", &finalizationFailure{Stage: "install", Reason: "install_script_not_regular"}
	}
	if _, err := gitFinalizationOutput(repoRoot, "ls-files", "--error-unmatch", "--", installScriptName); err != nil {
		return "", &finalizationFailure{Stage: "install", Reason: "install_script_untracked"}
	}
	return script, nil
}

func runInstallScript(script, repoRoot string, stdout, stderr io.Writer) error {
	command := exec.Command(script)
	command.Dir = repoRoot
	command.Stdout = stderr
	command.Stderr = stderr

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		return json.NewEncoder(stdout).Encode(installOutput{
			Status:  installStatusFailed,
			Failure: &finalizationFailure{Stage: "install", Reason: "install_script_start_failed", Detail: compactFinalizationDiagnostic(err.Error())},
		})
	}
	done := make(chan struct{})
	go forwardSignals(command.Process, signals, done)
	err := command.Wait()
	close(done)
	if err == nil {
		return json.NewEncoder(stdout).Encode(installOutput{Status: installStatusInstalled})
	}
	failure := &finalizationFailure{Stage: "install", Reason: "install_script_failed"}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		failure.ExitCode = childExitCode(exitErr)
	} else {
		failure.Detail = compactFinalizationDiagnostic(err.Error())
	}
	return json.NewEncoder(stdout).Encode(installOutput{Status: installStatusFailed, Failure: failure})
}
