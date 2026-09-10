package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestBuildProcessErrorExposesTerminalResultCorrectionEvidence(t *testing.T) {
	failure := &workflow.ResultCorrectionFailure{
		Reason:    "budget_exhausted",
		Attempts:  2,
		TaskID:    "task-id",
		SessionID: "session-id",
		Snapshot: state.SnapshotDigest{
			Head:           "head",
			IndexDigest:    "index",
			WorktreeDigest: "worktree",
		},
		Violations: []string{"first violation", "second violation"},
	}
	body := buildProcessError(workflow.NewResultCorrectionWorkerError("worker-new-result-correct-result-correct", failure))

	if body.Kind != errorKindWorkerError || body.Message == "" {
		t.Fatalf("worker error envelope = %#v", body)
	}
	if body.Detail["failure_class"] != "result_correction_budget_exhausted" || body.Detail["terminal"] != true || body.Detail["resume_available"] != false || body.Detail["additional_correction_available"] != false {
		t.Fatalf("terminal correction flags = %#v", body.Detail)
	}
	if body.Detail["correction_attempts"] != 2 || body.Detail["task_id"] != "task-id" || body.Detail["session_id"] != "session-id" {
		t.Fatalf("terminal correction identity = %#v", body.Detail)
	}
	violations, ok := body.Detail["violations"].([]string)
	if !ok || len(violations) != 2 {
		t.Fatalf("terminal correction violations = %#v", body.Detail["violations"])
	}
}
