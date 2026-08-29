package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentValidationFailureFixesBeforeIndependentReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: packetBody(packet.Result{
			Status:              packet.StatusImplemented,
			Risk:                packet.RiskLow,
			Summary:             "initial",
			RequirementCoverage: "covered",
			Tests:               "sandbox could not execute required process test",
			Unverified:          "parent process validation required",
			ParentValidation:    packet.ParentValidationGoTest,
		})},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	gateCalls := 0
	parentValidationGateRunner = func(_ *Workflow, form string) (parentValidationGateRecord, error) {
		gateCalls++
		if form != packet.ParentValidationGoTest {
			t.Fatalf("parent validation form = %q", form)
		}
		switch gateCalls {
		case 1:
			if !reflect.DeepEqual(r.phases, []string{"worker-new"}) {
				t.Fatalf("first gate ran after unexpected model phases: %v", r.phases)
			}
			return parentValidationGateRecord{
				ValidationRunID: "run-fail",
				Form:            form,
				Head:            "head-a",
				IndexDigest:     "index-a",
				WorktreeDigest:  "worktree-a",
				Status:          "fail",
				ExitCode:        1,
				Log:             "/evidence/run-fail/gate.log",
			}, nil
		case 2:
			if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1"}) {
				t.Fatalf("second gate ran after reviewer or unexpected model phase: %v", r.phases)
			}
			return parentValidationGateRecord{
				ValidationRunID: "run-pass",
				Form:            form,
				Head:            "head-a",
				IndexDigest:     "index-a",
				WorktreeDigest:  "worktree-b",
				Status:          "pass",
				Log:             "/evidence/run-pass/gate.log",
			}, nil
		default:
			t.Fatalf("unexpected parent validation call %d", gateCalls)
			return parentValidationGateRecord{}, nil
		}
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if gateCalls != 2 {
		t.Fatalf("parent validation calls = %d", gateCalls)
	}
	if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1", "reviewer-1-high-floor"}) {
		t.Fatalf("model phases = %v", r.phases)
	}
	if !strings.Contains(r.prompts[1], "validation_run_id=run-fail") {
		t.Fatalf("fix prompt lacks exact failed validation evidence: %s", r.prompts[1])
	}
	if !strings.Contains(r.prompts[2], "parent_validation_evidence") || !strings.Contains(r.prompts[2], "validation_run_id=run-pass") {
		t.Fatalf("review prompt lacks passed parent validation evidence: %s", r.prompts[2])
	}
}

func TestCheckpointParentValidationCannotBeDroppedOrChanged(t *testing.T) {
	checkpoint := stateCheckpointWithParentValidation(packet.ParentValidationGoTest)

	got, err := applyCheckpointParentValidation(checkpoint, packet.Result{
		Status: packet.StatusImplemented,
		Risk:   packet.RiskLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentValidation != packet.ParentValidationGoTest || got.Risk != packet.RiskHigh {
		t.Fatalf("checkpoint obligation was not preserved: %#v", got)
	}

	_, err = applyCheckpointParentValidation(checkpoint, packet.Result{
		Status:           packet.StatusImplemented,
		Risk:             packet.RiskLow,
		ParentValidation: packet.ParentValidationGoTestRace,
	})
	if err == nil {
		t.Fatal("worker changed the checkpoint-owned parent validation obligation")
	}
}

func stateCheckpointWithParentValidation(form string) state.ResumeCheckpoint {
	return state.ResumeCheckpoint{ParentValidation: form}
}
