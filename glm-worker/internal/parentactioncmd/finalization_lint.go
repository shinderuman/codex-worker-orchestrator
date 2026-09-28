package parentactioncmd

import (
	"encoding/json"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

var runFinalizationRepositoryLint = harnesslint.Run

func collectFinalizationRepositoryLint(repoRoot string) (json.RawMessage, *finalizationFailure) {
	report, err := runFinalizationRepositoryLint(repoRoot, false)
	if err != nil {
		return nil, &finalizationFailure{
			Stage: "repository_lint", Reason: "repository_lint_unavailable", Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return nil, &finalizationFailure{
			Stage: "repository_lint", Reason: "invalid_repository_lint_result", Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	lint := append(json.RawMessage(nil), payload...)
	if harnesslint.IsViolation(report) {
		return lint, &finalizationFailure{
			Stage: "repository_lint", Reason: "repository_lint_failed", Detail: compactFinalizationDiagnostic(string(payload)),
		}
	}
	if report.Status != finalizationValidationStatusPass || len(report.Violations) != 0 {
		return lint, &finalizationFailure{
			Stage: "repository_lint", Reason: "invalid_repository_lint_result", Detail: compactFinalizationDiagnostic(string(payload)),
		}
	}
	return lint, nil
}
