package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type QualityPreflightError struct {
	Tool           string
	Observed       string
	Required       string
	Classification string
	Cause          error
	DurationMS     int64
}

type qualityPreflightFunc func(root string) error

const qualityToolRepairEntry = "./install-quality-tools.sh"

const qualityToolVersionAuthority = "quality-tools.yml"

var runQualityPreflight qualityPreflightFunc = harnesslint.PreflightQualityTools

func (e *QualityPreflightError) Error() string {
	if e.Tool != "" {
		return fmt.Sprintf(
			"quality toolchain preflight failed before any model call (%s=%s, required=%s); run %s and rerun the same command",
			e.Tool, e.Observed, e.Required, qualityToolRepairEntry,
		)
	}
	return fmt.Sprintf(
		"quality toolchain preflight failed before any model call; run %s and rerun the same command: %v",
		qualityToolRepairEntry, e.Cause,
	)
}

func preflightQualityToolchain(cfg config.AppConfig, st *state.StateStore) error {
	if cfg.RepoRoot == "" || !harnesslint.AppliesTo(cfg.RepoRoot) {
		return nil
	}
	startedAt := time.Now().UTC()
	preflightErr := runQualityPreflight(cfg.RepoRoot)
	duration := time.Since(startedAt)
	recordPreflightAttempt(st, startedAt, duration, preflightErr)
	if preflightErr == nil {
		return nil
	}
	return newQualityPreflightError(preflightErr, duration)
}

func newQualityPreflightError(cause error, duration time.Duration) *QualityPreflightError {
	preflightErr := &QualityPreflightError{
		Classification: harnesslint.QualityToolInternalFailure,
		Cause:          cause,
		DurationMS:     duration.Milliseconds(),
	}
	var failure harnesslint.QualityToolFailure
	if errors.As(cause, &failure) {
		preflightErr.Classification = failure.QualityToolClassification()
	}
	var mismatch *harnesslint.QualityToolVersionMismatch
	if errors.As(cause, &mismatch) {
		preflightErr.Tool = mismatch.Tool
		preflightErr.Observed = mismatch.Observed
		preflightErr.Required = mismatch.Required
	}
	return preflightErr
}

func recordPreflightAttempt(st *state.StateStore, startedAt time.Time, duration time.Duration, preflightErr error) {
	outcome := state.PreflightOutcomePass
	if preflightErr != nil {
		outcome = state.PreflightOutcomeFail
	}
	st.RecordPreflightAttempt(outcome, startedAt, duration)
}
