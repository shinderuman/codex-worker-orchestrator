package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
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
	if bound, err := controller.ParentContinuationConfig(cfg); err == nil && bound.RepoRoot != cfg.RepoRoot {
		cfg = bound
		st = state.AttachStateStore(cfg)
	}
	output := buildParentHandoffFromContinuation(st, parentcontinuation.BuildWithRepositoryPolicy(cfg, st))
	applyCanonicalControllerHandoff(cfg, &output)
	return output
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
	full := buildParentHandoffWithConfig(cfg, st)
	value := projectParentHandoffRecovery(full)
	applyParentGuardRecovery(st, &value)
	applyParentQualityGateRecovery(st, &value)
	projected, err := projectParentHandoffRecoverySemantic(st, full, value)
	if err != nil {
		return err
	}
	digest, _ := parentevidence.Digest(projected)
	return parentevidence.FinishReadInScope(st, scope, state.ParentEvidenceSurfaceHandoffRecovery, digest, func() (int, error) {
		return parentevidence.WriteMeasuredJSON(stdout, projected)
	})
}

func printParentHandoffRoutingWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	value := buildParentHandoffWithConfig(cfg, st)
	_, err := parentevidence.WriteMeasuredJSON(stdout, value)
	return err
}
