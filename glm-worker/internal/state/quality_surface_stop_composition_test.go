package state

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestQualitySurfaceApprovalAllowsOnlyTransientResumeStops(t *testing.T) {
	for _, stopKind := range []ResumeStopKind{
		ResumeStopRateLimited,
		ResumeStopProviderUnavailable,
		ResumeStopInterrupted,
	} {
		t.Run(string(stopKind), func(t *testing.T) {
			checkpoint := qualitySurfaceStopCheckpoint()
			checkpoint.SetStopKind(stopKind)
			populateQualitySurfaceStopPayload(&checkpoint)
			if err := checkpoint.validateStopState(); err != nil {
				t.Fatalf("transient approval stop rejected: %v", err)
			}
			if checkpoint.StopGitSnapshot == nil || len(checkpoint.StopDirtyFiles) != 1 {
				t.Fatalf("approval retention lost across stop transition: %#v", checkpoint)
			}
		})
	}

	for _, stopKind := range []ResumeStopKind{ResumeStopGuardRecoverable, ResumeStopQualityGate} {
		t.Run(string(stopKind), func(t *testing.T) {
			checkpoint := qualitySurfaceStopCheckpoint()
			checkpoint.SetStopKind(stopKind)
			if err := checkpoint.validateStopState(); err == nil {
				t.Fatalf("non-transient approval stop %q unexpectedly admitted", stopKind)
			}
		})
	}
}

func TestStoppedQualitySurfaceApprovalRequiresCompletedResult(t *testing.T) {
	checkpoint := qualitySurfaceStopCheckpoint()
	checkpoint.CompletedResult = nil
	checkpoint.SetStopKind(ResumeStopRateLimited)
	if err := checkpoint.validateStopState(); err == nil {
		t.Fatal("stopped approval checkpoint without completed result unexpectedly admitted")
	}
}

func TestActivatedQualitySurfaceApprovalCanTransitionIntoTransientStop(t *testing.T) {
	checkpoint := qualitySurfaceStopCheckpoint()
	if err := checkpoint.MarkQualitySurfaceApprovalActivated(); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.validateStopState(); err != nil {
		t.Fatalf("in-flight activated approval checkpoint rejected: %v", err)
	}

	checkpoint.SetStopKind(ResumeStopRateLimited)
	if err := checkpoint.validateStopState(); err != nil {
		t.Fatalf("activated transient approval stop rejected: %v", err)
	}
	if err := checkpoint.ContinueActivatedQualitySurfaceApproval(); err != nil {
		t.Fatal(err)
	}
	if checkpoint.QualitySurfaceApprovalPending || checkpoint.QualitySurfaceApprovalActivated {
		t.Fatalf("activated continuation did not release approval facts: %#v", checkpoint)
	}
}

func TestQualitySurfaceApprovalActivationRequiresPendingCheckpoint(t *testing.T) {
	checkpoint := ResumeCheckpoint{Model: "worker"}
	if err := checkpoint.MarkQualitySurfaceApprovalActivated(); err == nil {
		t.Fatal("activation marker accepted a checkpoint without pending approval")
	}
}

func qualitySurfaceStopCheckpoint() ResumeCheckpoint {
	result := packet.Result{Status: packet.StatusImplemented}
	return ResumeCheckpoint{
		Model:                         "worker",
		CompletedResult:               &result,
		QualitySurfaceApprovalPending: true,
		StopGitSnapshot:               &GitSnapshot{Head: "0123456789abcdef0123456789abcdef01234567"},
		StopDirtyFiles:                []StopDirtyFile{{Path: "worker.go"}},
	}
}

func populateQualitySurfaceStopPayload(checkpoint *ResumeCheckpoint) {
	if checkpoint.StopKind != ResumeStopProviderUnavailable {
		return
	}
	checkpoint.ProviderUnavailableClassification = "http-503"
	checkpoint.ProviderUnavailableProbes = 1
	checkpoint.ProviderUnavailableStartedAt = time.Unix(1, 0).UTC()
}
