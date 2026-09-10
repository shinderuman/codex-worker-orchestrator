package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
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

type RepositoryHarnessPreflightError struct {
	Reason string
	Cause  error
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

func (e *RepositoryHarnessPreflightError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("repository harness quality scope invalid before any model call (%s): %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("repository harness quality scope invalid before any model call (%s)", e.Reason)
}

func (e *RepositoryHarnessPreflightError) Unwrap() error {
	return e.Cause
}

func preflightQualityToolchain(cfg config.AppConfig, st *state.StateStore) error {
	if cfg.RepoRoot == "" {
		return nil
	}
	qualityToolsApply, err := repositoryharness.QualityToolsApply(cfg.RepoRoot)
	if err != nil {
		return newRepositoryHarnessPreflightError(err)
	}
	if !qualityToolsApply {
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

func newRepositoryHarnessPreflightError(cause error) *RepositoryHarnessPreflightError {
	preflightErr := &RepositoryHarnessPreflightError{Reason: repositoryharness.QualityScopeEvaluationFailed, Cause: cause}
	var scopeErr *repositoryharness.QualityScopeError
	if errors.As(cause, &scopeErr) {
		preflightErr.Reason = scopeErr.Reason
	}
	return preflightErr
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
