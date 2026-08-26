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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var qualityGateForms = map[string][]string{
	"go-test":      {"test", "./..."},
	"go-test-race": {"test", "-race", "./..."},
}

const qualityGateLogDirectory = "quality-gate-logs"

type QualityGateError struct {
	Form       string
	Command    string
	WorkingDir string
	ExitCode   int
	DurationMS int64
	LogPath    string
}

func (e *QualityGateError) Error() string {
	return fmt.Sprintf("quality gateが失敗しました (exit %d)", e.ExitCode)
}

type qualityGateOutput struct {
	Status     string `json:"status"`
	Form       string `json:"form"`
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
	DurationMS int64  `json:"duration_ms"`
	Log        string `json:"log"`
}

func runQualityGate(form string, st *state.StateStore, stdout io.Writer) error {
	goArgs, ok := qualityGateForms[form]
	if !ok {
		return usageError("usage: glm-worker --quality-gate %s", qualityGateUsage)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("quality gateの作業dirを取得できません: %w", err)
	}
	command := "go " + strings.Join(goArgs, " ")
	startedAt := time.Now().UTC()
	var gateLog bytes.Buffer
	gate := exec.Command("go", goArgs...)
	gate.Dir = workingDir
	gate.Env = qualityGateEnv()
	gate.Stdout = &gateLog
	gate.Stderr = &gateLog
	runErr := gate.Run()
	durationMS := time.Now().UTC().Sub(startedAt).Milliseconds()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	result := "pass"
	if runErr != nil {
		result = "fail"
	}
	logPath := writeQualityGateLog(st, form, result, gateLog.Bytes())
	if runErr != nil {
		return &QualityGateError{
			Form:       form,
			Command:    command,
			WorkingDir: workingDir,
			ExitCode:   exitCode,
			DurationMS: durationMS,
			LogPath:    logPath,
		}
	}
	return writeJSON(stdout, qualityGateOutput{
		Status:     "pass",
		Form:       form,
		Command:    command,
		WorkingDir: workingDir,
		DurationMS: durationMS,
		Log:        logPath,
	})
}

func qualityGateEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOFLAGS=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOFLAGS=")
}

func writeQualityGateLog(st *state.StateStore, form string, result string, data []byte) string {
	path := st.Path(filepath.Join(qualityGateLogDirectory, form+"-"+result+".log"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ""
	}
	return path
}
