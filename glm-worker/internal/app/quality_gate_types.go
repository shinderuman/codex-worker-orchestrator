package app

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type QualityGateError struct {
	ValidationRunID string
	Form            string
	Command         string
	WorkingDir      string
	ExitCode        int
	DurationMS      int64
	LogPath         string
}

type qualityGateOutput struct {
	Status          string `json:"status"`
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Command         string `json:"command"`
	WorkingDir      string `json:"working_dir"`
	DurationMS      int64  `json:"duration_ms"`
	Log             string `json:"log"`
}

type qualityGateStartedEvent struct {
	Type            string `json:"type"`
	Event           string `json:"event"`
	ValidationRunID string `json:"validation_run_id"`
	Attached        bool   `json:"attached"`
}

type qualityGateRunnerWait func() error

type qualityGateRunnerLauncher func(*state.StateStore, qualitygate.RunRecord) (qualityGateRunnerWait, error)

type qualityGateStartIdentity struct {
	Form       string
	GoArgs     []string
	Repository string
	WorkingDir string
	TaskID     string
	Snapshot   state.GitSnapshot
}

const (
	qualityGateRunStateLock       = "state.lock"
	qualityGateRunnerStartupGrace = 30 * time.Second
)

var qualityGateForms = map[string][]string{
	"go-test":      {"test", "./..."},
	"go-test-race": {"test", "-race", "./..."},
}

var launchQualityGateRunner qualityGateRunnerLauncher = launchQualityGateRunnerProcess

func (e *QualityGateError) Error() string {
	return fmt.Sprintf("quality gateが失敗しました (exit %d)", e.ExitCode)
}
