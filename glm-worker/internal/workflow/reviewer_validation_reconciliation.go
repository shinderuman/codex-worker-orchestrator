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
	return result.Status == packet.StatusImplemented &&
		result.ParentValidationEvidence.ResolvedFor(result.ParentValidation)
}
