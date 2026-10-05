package app

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
)

type VerificationError struct {
	Outcome autoresume.Outcome
	Reason  string
}

type verifyAutoResumeOutput struct {
	AutomationKey  string `json:"automation_key"`
	TargetThread   string `json:"target_thread"`
	ExpectedAtUTC  string `json:"expected_at_utc"`
	TOMLDTStart    string `json:"toml_dtstart"`
	DBNextRunAtUTC string `json:"db_next_run_at_utc"`
}

func (e *VerificationError) Error() string {
	return fmt.Sprintf("verification %s: %s", outcomeLabel(e.Outcome), e.Reason)
}

func printVerifyCodexWake(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	key := autoresume.CodexWakeAutomationKey(cmd.Verify.ThreadID)
	return printAutomationVerification(key, cmd.Verify.RFC3339, cmd.Verify.ThreadID, cfg, stdout)
}

func printAutomationVerification(automationKey, expectedRFC3339, expectedThreadID string, cfg config.AppConfig, stdout io.Writer) error {
	params := autoresume.Params{
		AutomationKey:    automationKey,
		ExpectedRFC3339:  expectedRFC3339,
		ExpectedThreadID: expectedThreadID,
		AutomationsDir:   filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:           filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}
	result := autoresume.Verify(params, autoresume.ReadDBRowSqlite3)
	if result.Outcome != autoresume.Pass {
		return &VerificationError{Outcome: result.Outcome, Reason: result.Reason}
	}
	return machinecli.WriteJSON(stdout, verifyAutoResumeOutput{
		AutomationKey:  result.AutomationKey,
		TargetThread:   result.TargetThread,
		ExpectedAtUTC:  result.ExpectedUTC,
		TOMLDTStart:    result.TOMLDTStart,
		DBNextRunAtUTC: result.DBNextRunUTC,
	})
}

func outcomeLabel(o autoresume.Outcome) string {
	switch o {
	case autoresume.Pass:
		return "pass"
	case autoresume.Fail:
		return "fail"
	default:
		return "unavailable"
	}
}
