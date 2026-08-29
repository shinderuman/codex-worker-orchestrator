package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentValidationFailureFixesBeforeIndependentReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: packetBody(packet.Result{
			Status:                     packet.StatusImplemented,
			Risk:                       packet.RiskLow,
			Summary:                    "initial",
			RequirementCoverage:        "covered",
			Tests:                      "sandbox could not execute required process test",
			Unverified:                 "parent process validation required",
			ParentValidation:           packet.ParentValidationGoTest,
			ParentValidationWorkingDir: "glm-worker",
		})},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}

	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	gateCalls := 0
	parentValidationGateRunner = func(_ *Workflow, request packet.ParentValidationRequest) (parentValidationGateRecord, error) {
		gateCalls++
		if request.Form != packet.ParentValidationGoTest || request.WorkingDir != "glm-worker" {
			t.Fatalf("parent validation request = %#v", request)
		}
		record := parentValidationGateRecord{
			Form:           request.Form,
			Repository:     w.config.RepoRoot,
			WorkingDir:     workingDir,
			Head:           fixedSnapshot.Head,
			IndexDigest:    fixedSnapshot.IndexDigest,
			WorktreeDigest: fixedSnapshot.WorktreeDigest,
		}
		switch gateCalls {
		case 1:
			if !reflect.DeepEqual(r.phases, []string{"worker-new"}) {
				t.Fatalf("first gate ran after unexpected model phases: %v", r.phases)
			}
			record.ValidationRunID = "run-fail"
			record.Status = "fail"
			record.ExitCode = 1
			record.Log = "/evidence/run-fail/gate.log"
			return record, nil
		case 2:
			if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1"}) {
				t.Fatalf("second gate ran after reviewer or unexpected model phase: %v", r.phases)
			}
			record.ValidationRunID = "run-pass"
			record.Status = "pass"
			record.Log = "/evidence/run-pass/gate.log"
			return record, nil
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
	checkpoint := stateCheckpointWithParentValidation(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	})

	got, err := applyCheckpointParentValidation(checkpoint, packet.Result{
		Status: packet.StatusImplemented,
		Risk:   packet.RiskLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotRequest := got.ParentValidationRequest()
	if gotRequest == nil || !sameParentValidationRequest(*gotRequest, *checkpoint.ParentValidation) || got.Risk != packet.RiskHigh {
		t.Fatalf("checkpoint obligation was not preserved: %#v", got)
	}

	_, err = applyCheckpointParentValidation(checkpoint, packet.Result{
		Status:                     packet.StatusImplemented,
		Risk:                       packet.RiskLow,
		ParentValidation:           packet.ParentValidationGoTestRace,
		ParentValidationWorkingDir: "glm-worker",
	})
	if err == nil {
		t.Fatal("worker changed the checkpoint-owned parent validation obligation")
	}
}

func TestParentValidationRecordRejectsStaleSnapshot(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := parentValidationGateRecord{
		ValidationRunID: "run-pass",
		Form:            packet.ParentValidationGoTest,
		Repository:      w.config.RepoRoot,
		WorkingDir:      workingDir,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  "stale-worktree",
		Status:          "pass",
	}
	err := w.validateParentValidationRecord(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	}, record)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("stale parent validation evidence was accepted: %v", err)
	}
}

func stateCheckpointWithParentValidation(request packet.ParentValidationRequest) state.ResumeCheckpoint {
	return state.ResumeCheckpoint{ParentValidation: cloneParentValidationRequest(&request)}
}
