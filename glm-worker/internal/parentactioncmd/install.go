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
	"strings"
	"sync"
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

type runtimeInstallAttempt struct {
	requirement runtimeInstallRequirement
	taskID      string
	output      installOutput
}

type installDiagnosticTail struct {
	mu   sync.Mutex
	data []byte
}

type installFailureEnvelope struct {
	Failure *finalizationFailure `json:"failure"`
}

const (
	installStatusInstalled          = "installed"
	installStatusNotRequired        = "not_required"
	installStatusFailed             = "install_failed"
	installStatusVerificationFailed = "install_verification_failed"
	installStatusGuardRejected      = "install_guard_rejected"
	installScriptName               = "install.sh"
)

func (w *installDiagnosticTail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if n >= finalizationDiagnosticLimit {
		w.data = append(w.data[:0], p[n-finalizationDiagnosticLimit:]...)
		return n, nil
	}
	if overflow := len(w.data) + n - finalizationDiagnosticLimit; overflow > 0 {
		copy(w.data, w.data[overflow:])
		w.data = w.data[:len(w.data)-overflow]
	}
	w.data = append(w.data, p...)
	return n, nil
}

func (w *installDiagnosticTail) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(append([]byte(nil), w.data...))
}

func executeInstall(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action install")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	attempt, err := runRuntimeInstallAttemptLocked(cfg, st, stderr)
	if err != nil {
		return err
	}
	if attempt.output.Status != installStatusInstalled || !attempt.output.Required {
		return writeInstallOutput(stdout, attempt.output)
	}
	if failure := runRuntimeInstallSmoke(cfg); failure != nil {
		return writeInstallOutput(stdout, installOutput{Status: installStatusVerificationFailed, Required: true, Failure: failure})
	}
	return finalizeRuntimeInstallAfterSmokeLocked(cfg, st, attempt, stdout)
}

func runRuntimeInstallAttemptLocked(cfg config.AppConfig, st *state.StateStore, stderr io.Writer) (runtimeInstallAttempt, error) {
	plan, err := st.ParentActionPlan()
	if err != nil {
		return runtimeInstallAttempt{}, err
	}
	if !plan.Allows(state.ParentActionInstall) {
		return runtimeInstallAttempt{}, fmt.Errorf("install is not admitted for the current task (required action %s)", plan.RequiredAction)
	}
	if failure := installRepositoryGuard(cfg.RepoRoot, st); failure != nil {
		return runtimeInstallAttempt{output: installOutput{Status: installStatusGuardRejected, Failure: failure}}, nil
	}
	script, failure := installScriptGuard(cfg.RepoRoot)
	if failure != nil {
		return runtimeInstallAttempt{output: installOutput{Status: installStatusGuardRejected, Failure: failure}}, nil
	}
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		return runtimeInstallAttempt{output: installOutput{
			Status:  installStatusGuardRejected,
			Failure: runtimeInstallFailure(runtimeInstallFailureClassification, err.Error()),
		}}, nil
	}
	if !requirement.Required {
		return runtimeInstallAttempt{output: installOutput{Status: installStatusNotRequired}}, nil
	}
	taskID, err := st.TaskID()
	if err != nil {
		return runtimeInstallAttempt{output: installOutput{
			Status:   installStatusVerificationFailed,
			Required: true,
			Failure:  runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error()),
		}}, nil
	}
	if !pushBindingTreeClean(cfg.RepoRoot) {
		return runtimeInstallAttempt{output: installOutput{
			Status:   installStatusGuardRejected,
			Required: true,
			Failure:  runtimeInstallFailure(runtimeInstallFailureDirty, "runtime install requires a clean committed source tree"),
		}}, nil
	}
	if err := st.ClearRuntimeInstallEvidence(); err != nil {
		return runtimeInstallAttempt{output: installOutput{
			Status:   installStatusVerificationFailed,
			Required: true,
			Failure:  runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error()),
		}}, nil
	}
	output := runInstallScript(script, cfg.RepoRoot, stderr)
	output.Required = true
	return runtimeInstallAttempt{requirement: requirement, taskID: taskID, output: output}, nil
}

func finalizeRuntimeInstallAfterSmokeLocked(cfg config.AppConfig, st *state.StateStore, attempt runtimeInstallAttempt, stdout io.Writer) error {
	if failure := validateRuntimeInstallPostSmoke(st, attempt.taskID); failure != nil {
		return writeInstallOutput(stdout, installOutput{Status: installStatusVerificationFailed, Required: true, Failure: failure})
	}
	if failure := persistRuntimeInstallCompletionAfterSmoke(cfg, st, attempt.requirement); failure != nil {
		return writeInstallOutput(stdout, installOutput{Status: installStatusVerificationFailed, Required: true, Failure: failure})
	}
	return writeInstallOutput(stdout, installOutput{Status: installStatusInstalled, Required: true})
}

func validateRuntimeInstallPostSmoke(st *state.StateStore, expectedTaskID string) *finalizationFailure {
	plan, err := st.ParentActionPlan()
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureStale, err.Error())
	}
	if !plan.Allows(state.ParentActionInstall) {
		return runtimeInstallFailure(runtimeInstallFailureStale, "task state changed while install smoke was running")
	}
	currentTaskID, err := st.TaskID()
	if err != nil || currentTaskID != expectedTaskID {
		return runtimeInstallFailure(runtimeInstallFailureStale, "task identity changed while install smoke was running")
	}
	return nil
}

func installRepositoryGuard(repoRoot string, st *state.StateStore) *finalizationFailure {
	active, err := repositoryharness.RuntimeActive(repoRoot, st)
	if err != nil {
		return &finalizationFailure{Stage: "install", Reason: "repository_harness_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if !active {
		return &finalizationFailure{Stage: "install", Reason: "repository_harness_inactive", Detail: "repository harness activation pin is inactive"}
	}
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
	diagnostic := &installDiagnosticTail{}
	childOutput := io.MultiWriter(stderr, diagnostic)
	command.Stdout = childOutput
	command.Stderr = childOutput

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

	detail := compactFinalizationDiagnostic(diagnostic.String())
	failure := &finalizationFailure{Stage: "install", Reason: "install_script_failed", Detail: detail}
	if typed := installTypedChildFailure(diagnostic.String()); typed != nil {
		failure.Stage = typed.Stage
		failure.Reason = typed.Reason
		if typed.Detail != "" {
			failure.Detail = compactFinalizationDiagnostic(typed.Detail)
		}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		failure.ExitCode = childExitCode(exitErr)
	}
	if failure.Detail == "" {
		failure.Detail = compactFinalizationDiagnostic(err.Error())
	}
	return installOutput{Status: installStatusFailed, Failure: failure}
}

func installTypedChildFailure(value string) *finalizationFailure {
	lines := strings.Split(value, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		var envelope installFailureEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil || envelope.Failure == nil {
			continue
		}
		if envelope.Failure.Stage == "" || envelope.Failure.Reason == "" {
			continue
		}
		failure := *envelope.Failure
		failure.Detail = compactFinalizationDiagnostic(failure.Detail)
		return &failure
	}
	return nil
}
