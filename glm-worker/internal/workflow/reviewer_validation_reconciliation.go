package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

const reviewerUnverifiedReferenceMarker = "WORKER_UNVERIFIED_REFERENCE_ONLY:"

func reconcileReviewerWorkerReport(workerReport string) (string, string) {
	result, err := packet.ParseStructured([]byte(workerReport))
	if err != nil || !reviewerParentValidationResolved(result) || strings.TrimSpace(result.Unverified) == "" {
		return workerReport, ""
	}

	originalUnverified := result.Unverified
	result.Unverified = fmt.Sprintf(
		"parent validation %s is resolved by wrapper-validated current exact-snapshot PASS evidence; unrelated worker claims remain reference-only below",
		result.ParentValidation,
	)
	normalized, err := result.MachineJSON()
	if err != nil {
		return workerReport, ""
	}

	context := fmt.Sprintf(`PARENT_VALIDATION_RECONCILIATION:
STATUS: resolved-current-snapshot
FORM: %s
AUTHORITY: wrapper-parent-validation-evidence
%s %s
RULE: original worker prose is reference evidence, not current validation status; preserve unrelated concerns for independent review and do not repeat the resolved parent-validation obligation as unverified.
END_PARENT_VALIDATION_RECONCILIATION`, result.ParentValidation, reviewerUnverifiedReferenceMarker, originalUnverified)
	return string(normalized), context
}

func reviewerParentValidationResolved(result packet.Result) bool {
	if result.Status != packet.StatusImplemented || result.ParentValidationEvidence == "" {
		return false
	}
	if result.ParentValidation != packet.ParentValidationGoTest && result.ParentValidation != packet.ParentValidationGoTestRace {
		return false
	}

	evidence := result.ParentValidationEvidence
	return parentValidationEvidenceValue(evidence, "status") == "pass" &&
		parentValidationEvidenceValue(evidence, "form") == result.ParentValidation &&
		parentValidationEvidenceValue(evidence, "validation_run_id") != "" &&
		parentValidationEvidenceValue(evidence, "head") != "" &&
		parentValidationEvidenceValue(evidence, "index") != "" &&
		parentValidationEvidenceValue(evidence, "worktree") != ""
}

func parentValidationEvidenceValue(evidence, key string) string {
	prefix := key + "="
	start := -1
	if strings.HasPrefix(evidence, prefix) {
		start = len(prefix)
	} else if index := strings.Index(evidence, ";"+prefix); index >= 0 {
		start = index + len(prefix) + 1
	}
	if start < 0 || start >= len(evidence) {
		return ""
	}
	end := strings.IndexByte(evidence[start:], ';')
	if end < 0 {
		return evidence[start:]
	}
	return evidence[start : start+end]
}
