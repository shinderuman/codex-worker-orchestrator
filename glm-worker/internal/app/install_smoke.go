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

type InstallSmokeError struct {
	Role       string
	ExitCode   int
	DurationMS int64
}

type installSmokeOutput struct {
	Status     string `json:"status"`
	Result     string `json:"result"`
	Role       string `json:"role,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

var validInstallSmokeRoles = map[string]bool{
	"worker":   true,
	"reviewer": true,
	"fix":      true,
	"parent":   true,
}

func (e *InstallSmokeError) Error() string {
	return fmt.Sprintf("install smokeが失敗しました (exit %d)", e.ExitCode)
}

func runInstallSmoke(role string, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
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
		durationMS := time.Since(started).Milliseconds()
		st.RecordValidation("install-smoke", "install-smoke", role, "fail", exitCode, durationMS, "")
		return &InstallSmokeError{Role: role, ExitCode: exitCode, DurationMS: durationMS}
	}
	durationMS := time.Since(started).Milliseconds()
	st.RecordValidation("install-smoke", "install-smoke", role, "pass", 0, durationMS, "")
	return writeJSON(stdout, installSmokeOutput{
		Status:     "executed",
		Result:     "pass",
		Role:       role,
		DurationMS: durationMS,
	})
}
