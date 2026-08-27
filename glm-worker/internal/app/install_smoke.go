package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var validInstallSmokeRoles = map[string]bool{
	"worker":   true,
	"reviewer": true,
	"fix":      true,
	"parent":   true,
}

type InstallSmokeError struct {
	Role             string
	ExitCode         int
	ReuseReason      string
	DurationMS       int64
	LogPath          string
	TreeDigest       string
	SmokeInputDigest string
}

func (e *InstallSmokeError) Error() string {
	return fmt.Sprintf("install smokeが失敗しました (exit %d)", e.ExitCode)
}

type installSmokeOutput struct {
	Status     string `json:"status"`
	Result     string `json:"result"`
	Role       string `json:"role,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func runInstallSmoke(role string, cfg config.AppConfig, _ *state.StateStore, stdout io.Writer) error {
	script := filepath.Join(cfg.RepoRoot, "tests", "install_smoke.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("install smoke scriptがありません: %s: %w", script, err)
	}
	started := time.Now()
	command := exec.Command(script)
	command.Dir = cfg.RepoRoot
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return &InstallSmokeError{Role: role, ExitCode: exitCode, DurationMS: time.Since(started).Milliseconds()}
	}
	return writeJSON(stdout, installSmokeOutput{
		Status:     "executed",
		Result:     "pass",
		Role:       role,
		DurationMS: time.Since(started).Milliseconds(),
	})
}
