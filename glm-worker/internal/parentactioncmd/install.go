package parentactioncmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type installOutput struct {
	Status   string               `json:"status"`
	Required bool                 `json:"required"`
	Failure  *finalizationFailure `json:"failure,omitempty"`
}

type installDiagnosticTail struct {
	mu   sync.Mutex
	data []byte
}

type installFailureEnvelope struct {
	Failure *finalizationFailure `json:"failure"`
}

const (
	installStatusInstalled = "installed"
	installStatusFailed    = "install_failed"
	installScriptName      = "install.sh"
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
