package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type projectContinuationAutomation struct {
	AutomationID string `json:"automation_id"`
	ParentThread string `json:"parent_thread"`
	WakeThread   string `json:"wake_thread"`
	ResumeAtUTC  string `json:"resume_at_utc"`
	WakeAtUTC    string `json:"wake_at_utc"`
}

func buildParentHandoffWithConfig(cfg config.AppConfig, st *state.StateStore) parentHandoffOutput {
	return buildParentHandoffFromContinuation(st, parentcontinuation.Build(cfg, st))
}

func printParentHandoffLeasedWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	scope, err := parentevidence.CaptureReadScope(st)
	if err != nil {
		return err
	}
	value := buildParentHandoffWithConfig(cfg, st)
	digest, _ := parentevidence.Digest(value)
	return parentevidence.FinishReadInScope(st, scope, state.ParentEvidenceSurfaceHandoff, digest, func() (int, error) {
		return parentevidence.WriteMeasuredJSON(stdout, value)
	})
}

func printParentHandoffRecoveryLeasedWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	scope, err := parentevidence.CaptureReadScope(st)
	if err != nil {
		return err
	}
	value := projectParentHandoffRecovery(buildParentHandoffWithConfig(cfg, st))
	applyParentGuardRecovery(st, &value)
	applyParentQualityGateRecovery(st, &value)
	digest, _ := parentevidence.Digest(value)
	return parentevidence.FinishReadInScope(st, scope, state.ParentEvidenceSurfaceHandoffRecovery, digest, func() (int, error) {
		return parentevidence.WriteMeasuredJSON(stdout, value)
	})
}
