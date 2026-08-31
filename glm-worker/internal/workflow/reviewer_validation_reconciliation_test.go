package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewerPromptReconcilesCurrentParentValidationEvidence(t *testing.T) {
	const originalUnverified = "full go test was not run after the fix; unrelated manual compatibility check remains"
	result := packet.Result{
		Status:                     packet.StatusImplemented,
		Risk:                       packet.RiskLow,
		Summary:                    "implemented",
		RequirementCoverage:        "covered",
		Tests:                      "targeted tests passed",
		Unverified:                 originalUnverified,
		ParentValidation:           packet.ParentValidationGoTest,
		ParentValidationWorkingDir: "glm-worker",
		ParentValidationEvidence: parentValidationEvidence(parentValidationGateRecord{
			ValidationRunID: "run-pass",
			Form:            packet.ParentValidationGoTest,
			WorkingDir:      "/repo/glm-worker",
			Head:            "head",
			IndexDigest:     "index",
			WorktreeDigest:  "worktree",
			Status:          "pass",
		}),
	}
	report, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}

	prompt := reviewerPrompt("request", "none", string(report), 1, "baseline", "navigation", "")
	marker := "\n\nPARENT_VALIDATION_RECONCILIATION:"
	markerIndex := strings.Index(prompt, marker)
	if markerIndex < 0 {
		t.Fatalf("reviewer prompt lacks reconciliation block: %s", prompt)
	}
	workerStart := strings.Index(prompt, "WORKER_REPORT:\n") + len("WORKER_REPORT:\n")
	workerProjection := prompt[workerStart:markerIndex]
	if strings.Contains(workerProjection, originalUnverified) {
		t.Fatalf("authoritative worker projection retained stale unverified prose: %s", workerProjection)
	}
	if !strings.Contains(workerProjection, "parent validation go-test is resolved") || !strings.Contains(workerProjection, "validation_run_id=run-pass") {
		t.Fatalf("worker projection lacks resolved current validation evidence: %s", workerProjection)
	}
	if !strings.Contains(prompt[markerIndex:], reviewerUnverifiedReferenceMarker+" "+originalUnverified) {
		t.Fatalf("original unrelated/ambiguous worker prose was not preserved as reference: %s", prompt[markerIndex:])
	}
}

func TestReviewerPromptDoesNotReconcileMissingFailedOrMismatchedEvidence(t *testing.T) {
	const originalUnverified = "parent validation still unverified"
	cases := map[string]string{
		"missing":       "",
		"failed":        "status=fail;form=go-test;validation_run_id=run;head=head;index=index;worktree=worktree",
		"form-mismatch": "status=pass;form=go-test-race;validation_run_id=run;head=head;index=index;worktree=worktree",
	}
	for name, evidence := range cases {
		t.Run(name, func(t *testing.T) {
			result := packet.Result{
				Status:                     packet.StatusImplemented,
				Risk:                       packet.RiskLow,
				Summary:                    "implemented",
				RequirementCoverage:        "covered",
				Tests:                      "targeted tests passed",
				Unverified:                 originalUnverified,
				ParentValidation:           packet.ParentValidationGoTest,
				ParentValidationWorkingDir: "glm-worker",
				ParentValidationEvidence:   evidence,
			}
			report, err := result.MachineJSON()
			if err != nil {
				t.Fatal(err)
			}
			prompt := reviewerPrompt("request", "none", string(report), 1, "baseline", "navigation", "")
			if strings.Contains(prompt, "PARENT_VALIDATION_RECONCILIATION:") {
				t.Fatalf("non-authoritative evidence was reconciled: %s", prompt)
			}
			if !strings.Contains(prompt, originalUnverified) {
				t.Fatalf("unverified evidence disappeared: %s", prompt)
			}
		})
	}
}

func TestCheckpointMutationClearsPriorValidationBeforeReviewerReconciliation(t *testing.T) {
	checkpoint := state.ResumeCheckpoint{ParentValidation: &packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	}}
	result, err := applyCheckpointParentValidation(checkpoint, packet.Result{
		Status:                     packet.StatusImplemented,
		Risk:                       packet.RiskLow,
		Summary:                    "changed snapshot",
		RequirementCoverage:        "covered",
		Tests:                      "targeted tests passed",
		Unverified:                 "full gate must be rerun for the changed snapshot",
		ParentValidation:           packet.ParentValidationGoTest,
		ParentValidationWorkingDir: "glm-worker",
		ParentValidationEvidence:   "status=pass;form=go-test;validation_run_id=stale;head=old;index=old;worktree=old",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ParentValidationEvidence != "" {
		t.Fatalf("changed snapshot retained stale parent validation evidence: %q", result.ParentValidationEvidence)
	}
	report, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	prompt := reviewerPrompt("request", "none", string(report), 1, "baseline", "navigation", "")
	if strings.Contains(prompt, "PARENT_VALIDATION_RECONCILIATION:") {
		t.Fatalf("changed snapshot reconciled stale evidence: %s", prompt)
	}
}
