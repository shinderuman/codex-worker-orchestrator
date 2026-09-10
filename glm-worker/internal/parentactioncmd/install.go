package parentactioncmd

import (
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
	Status   string               `json:"status"`
	Required bool                 `json:"required"`
	Failure  *finalizationFailure `json:"failure,omitempty"`
}

const (
	installStatusInstalled          = "installed"
	installStatusNotRequired        = "not_required"
	installStatusFailed             = "install_failed"
	installStatusVerificationFailed = "install_verification_failed"
	installStatusGuardRejected      = "install_guard_rejected"
	installScriptName               = "install.sh"
)

func executeInstall(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action install")
	}
	st := state.AttachStateStore(cfg)
	var requirement runtimeInstallRequirement
	var expectedTaskID string
	output, err := func() (installOutput, error) {
		lock, err := repolock.Acquire(st.LockPath())
		if err != nil {
			return installOutput{}, err
		}
		defer func() { _ = lock.Close() }()

		plan, err := st.ParentActionPlan()
		if err != nil {
			return installOutput{}, err
		}
		if !plan.Allows(state.ParentActionInstall) {
			return installOutput{}, fmt.Errorf("install is not admitted for the current task (required action %s)", plan.RequiredAction)
		}
		if failure := installRepositoryGuard(cfg.RepoRoot); failure != nil {
			return installOutput{Status: installStatusGuardRejected, Failure: failure}, nil
		}
		script, failure := installScriptGuard(cfg.RepoRoot)
		if failure != nil {
			return installOutput{Status: installStatusGuardRejected, Failure: failure}, nil
		}
		requirement, err = runtimeInstallRequirementForTask(cfg.RepoRoot, st)
		if err != nil {
			return installOutput{
				Status:  installStatusGuardRejected,
				Failure: runtimeInstallFailure(runtimeInstallFailureClassification, err.Error()),
			}, nil
		}
		if !requirement.Required {
			return installOutput{Status: installStatusNotRequired}, nil
		}
		expectedTaskID, err = st.TaskID()
		if err != nil {
			return installOutput{
				Status:   installStatusVerificationFailed,
				Required: true,
				Failure:  runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error()),
			}, nil
		}
		if !pushBindingTreeClean(cfg.RepoRoot) {
			return installOutput{
				Status:   installStatusGuardRejected,
				Required: true,
				Failure:  runtimeInstallFailure(runtimeInstallFailureDirty, "runtime install requires a clean committed source tree"),
			}, nil
		}
		if err := st.ClearRuntimeInstallEvidence(); err != nil {
			return installOutput{
				Status:   installStatusVerificationFailed,
				Required: true,
				Failure:  runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error()),
			}, nil
		}
		result := runInstallScript(script, cfg.RepoRoot, stderr)
		result.Required = true
		return result, nil
	}()
	if err != nil {
		return err
	}
	if output.Status != installStatusInstalled || !output.Required {
		return writeInstallOutput(stdout, output)
	}

	if failure := runRuntimeInstallSmoke(cfg); failure != nil {
		return writeInstallOutput(stdout, installOutput{Status: installStatusVerificationFailed, Required: true, Failure: failure})
	}

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
		return writeInstallOutput(stdout, installOutput{
			Status:   installStatusVerificationFailed,
			Required: true,
			Failure:  runtimeInstallFailure(runtimeInstallFailureStale, "task state changed while install smoke was running"),
		})
	}
	currentTaskID, err := st.TaskID()
	if err != nil || currentTaskID != expectedTaskID {
		return writeInstallOutput(stdout, installOutput{
			Status:   installStatusVerificationFailed,
			Required: true,
			Failure:  runtimeInstallFailure(runtimeInstallFailureStale, "task identity changed while install smoke was running"),
		})
	}
	if failure := persistRuntimeInstallCompletionAfterSmoke(cfg, st, requirement); failure != nil {
		return writeInstallOutput(stdout, installOutput{Status: installStatusVerificationFailed, Required: true, Failure: failure})
	}
	return writeInstallOutput(stdout, installOutput{Status: installStatusInstalled, Required: true})
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

func runInstallScript(script, repoRoot string, stderr io.Writer) installOutput {
	command := exec.Command(script)
	command.Dir = repoRoot
	command.Stdout = stderr
	command.Stderr = stderr

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		return installOutput{
			Status:  installStatusFailed,
			Failure: &finalizationFailure{Stage: "install", Reason: "install_script_start_failed", Detail: compactFinalizationDiagnostic(err.Error())},
		}
	}
	done := make(chan struct{})
	go forwardSignals(command.Process, signals, done)
	err := command.Wait()
	close(done)
	if err == nil {
		return installOutput{Status: installStatusInstalled}
	}
	failure := &finalizationFailure{Stage: "install", Reason: "install_script_failed"}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		failure.ExitCode = childExitCode(exitErr)
	} else {
		failure.Detail = compactFinalizationDiagnostic(err.Error())
	}
	return installOutput{Status: installStatusFailed, Failure: failure}
}
