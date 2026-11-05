package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
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
	if applyAvailableSummaryRecoveryHandoff(cfg, st, &output) {
		return output
	}
	applyCanonicalControllerHandoff(cfg, &output)
	applyReportResubmissionAvailability(cfg, st, &output)
	return output
}

func applyAvailableSummaryRecoveryHandoff(cfg config.AppConfig, st *state.StateStore, output *parentHandoffOutput) bool {
	if !output.Consistent || output.RequiredAction == nil ||
		*output.RequiredAction != string(state.ParentActionResubmitReport) ||
		(output.ParentRequest != nil && !output.ParentRequest.TaskAttribution.Matches) {
		return false
	}
	wf := workflow.NewWorkflow(cfg, st, nil, io.Discard)
	if wf.SummaryRecoveryAvailable() != nil {
		return false
	}
	applyCanonicalControllerHandoff(cfg, output)
	return true
}

func applyReportResubmissionAvailability(cfg config.AppConfig, st *state.StateStore, output *parentHandoffOutput) {
	if output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionResubmitReport) {
		return
	}
	wf := workflow.NewWorkflow(cfg, st, nil, io.Discard)
	if err := wf.ReportResubmissionAvailable(); err != nil {
		reason := "report resubmission unavailable: " + err.Error()
		output.Consistent = false
		output.Inconsistency = &reason
		output.RequiredAction = nil
		output.AllowedActions = nil
		output.RequiredActionParameters = nil
		output.ParentRequest = nil
	}
}

func printParentHandoffLeasedWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	return printParentHandoffLeased(st, stdout, func() (parentHandoffOutput, error) {
		return buildParentHandoffWithConfig(cfg, st), nil
	})
}

func printParentHandoffLeased(st *state.StateStore, stdout io.Writer, project func() (parentHandoffOutput, error)) error {
	scope, err := parentevidence.CaptureReadScope(st)
	if err != nil {
		return err
	}
	value, err := project()
	if err != nil {
		return err
	}
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
