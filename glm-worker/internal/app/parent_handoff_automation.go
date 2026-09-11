package app

import (
	"io"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type projectContinuationAutomation struct {
	AutomationID string `json:"automation_id"`
	ParentThread string `json:"parent_thread"`
	WakeThread   string `json:"wake_thread"`
	ResumeAtUTC  string `json:"resume_at_utc"`
	WakeAtUTC    string `json:"wake_at_utc"`
}

const projectContinuationReasonVerifiedAutomation = "verified-automation"

func buildParentHandoffWithConfig(cfg config.AppConfig, st *state.StateStore) parentHandoffOutput {
	output := buildParentHandoff(st)
	applyVerifiedAutomationDeferral(cfg, st, &output, autoresume.ReadDBRowSqlite3)
	return output
}

func printParentHandoffLeasedWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	scope, err := captureParentEvidenceReadScope(st)
	if err != nil {
		return err
	}
	value := buildParentHandoffWithConfig(cfg, st)
	digest, _ := parentEvidenceDigest(value)
	return finishParentReadInScope(st, scope, state.ParentEvidenceSurfaceHandoff, digest, func() (int, error) {
		return writeMeasuredJSON(stdout, value)
	})
}

func printParentHandoffRecoveryLeasedWithConfig(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	scope, err := captureParentEvidenceReadScope(st)
	if err != nil {
		return err
	}
	value := projectParentHandoffRecovery(buildParentHandoffWithConfig(cfg, st))
	applyParentGuardRecovery(st, &value)
	applyParentQualityGateRecovery(st, &value)
	digest, _ := parentEvidenceDigest(value)
	return finishParentReadInScope(st, scope, state.ParentEvidenceSurfaceHandoffRecovery, digest, func() (int, error) {
		return writeMeasuredJSON(stdout, value)
	})
}

func applyVerifiedAutomationDeferral(cfg config.AppConfig, st *state.StateStore, output *parentHandoffOutput, readDB autoresume.DBReader) {
	if output.ParentRequest == nil ||
		output.ParentRequest.Continuation.State != projectContinuationBlocked ||
		output.ParentRequest.Continuation.Reason != string(state.TaskStatusRateLimited) {
		return
	}
	output.ParentRequest.StopAdmitted = false
	proof, ok := verifiedAutomationDeferral(cfg, st, readDB)
	if !ok {
		return
	}
	output.ParentRequest.Continuation.State = projectContinuationDeferredByVerifiedAutomation
	output.ParentRequest.Continuation.Reason = projectContinuationReasonVerifiedAutomation
	output.ParentRequest.Continuation.Automation = &proof
	output.ParentRequest.StopAdmitted = true
}

func verifiedAutomationDeferral(cfg config.AppConfig, st *state.StateStore, readDB autoresume.DBReader) (projectContinuationAutomation, bool) {
	if cfg.CodexConfigDir == "" {
		return projectContinuationAutomation{}, false
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopRateLimited || checkpoint.ResetAtRFC3339 == "" {
		return projectContinuationAutomation{}, false
	}
	identity, err := st.CurrentParentCodexIdentity()
	if err != nil || identity.ThreadID == "" {
		return projectContinuationAutomation{}, false
	}
	result, err := autoresume.CheckCoalesce(autoresume.CoalesceParams{
		ParentThreadID:  identity.ThreadID,
		ResumeAtRFC3339: checkpoint.ResetAtRFC3339,
		AutomationsDir:  filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:          filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}, readDB)
	if err != nil || result.Decision != autoresume.DecisionCoalesce {
		return projectContinuationAutomation{}, false
	}
	return projectContinuationAutomation{
		AutomationID: result.WakeAutomationID,
		ParentThread: result.ParentThread,
		WakeThread:   result.WakeThread,
		ResumeAtUTC:  result.ResumeAtUTC,
		WakeAtUTC:    result.WakeNextRunUTC,
	}, true
}
