package app

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"time"
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

type qualityGateRunRecord = qualitygate.RunRecord

type qualityGateStartedEvent struct {
	Type            string `json:"type"`
	Event           string `json:"event"`
	ValidationRunID string `json:"validation_run_id"`
	Attached        bool   `json:"attached"`
}

type qualityGateRunnerWait func() error

type qualityGateRunnerLauncher func(*state.StateStore, qualityGateRunRecord) (qualityGateRunnerWait, error)

type qualityGateStartIdentity struct {
	Form       string
	GoArgs     []string
	Repository string
	WorkingDir string
	TaskID     string
	Snapshot   state.GitSnapshot
}

const (
	qualityGateRunDirectory       = qualitygate.RunDirectory
	qualityGateRunFile            = qualitygate.RunFile
	qualityGateRunLog             = qualitygate.RunLog
	qualityGateRunStateLock       = "state.lock"
	qualityGateStatusRunning      = qualitygate.StatusRunning
	qualityGateStatusPass         = qualitygate.StatusPass
	qualityGateStatusFail         = qualitygate.StatusFail
	qualityGateStatusInterrupted  = qualitygate.StatusInterrupted
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
