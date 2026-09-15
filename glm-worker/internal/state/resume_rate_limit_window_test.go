package state

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdmitParentActionRejectsResumeBeforeRateLimitReset(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeRateLimitedWindowCheckpoint(t, st, time.Now().Add(time.Hour).Format(time.RFC3339))

	_, admitted, err := st.AdmitParentAction(ParentActionResume)
	if err == nil || admitted {
		t.Fatalf("resume admission = admitted:%v err:%v", admitted, err)
	}
	if !strings.Contains(err.Error(), "cannot resume before the Z.ai 5h reset at") {
		t.Fatalf("error = %v", err)
	}
	if status := st.TaskStatus(); status != TaskStatusRateLimited {
		t.Fatalf("rejected resume must keep the stopped task state, got %s", status)
	}
}

func TestAdmitParentActionAllowsResumeAfterRateLimitReset(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeRateLimitedWindowCheckpoint(t, st, time.Now().Add(-time.Minute).Format(time.RFC3339))

	_, admitted, err := st.AdmitParentAction(ParentActionResume)
	if err != nil || !admitted {
		t.Fatalf("manual resume after the reset = admitted:%v err:%v", admitted, err)
	}
}

func TestAdmitParentActionResumeWindowBindsOnlyRateLimitResetEvidence(t *testing.T) {
	unknown := &StateStore{dir: t.TempDir()}
	writeRateLimitedWindowCheckpoint(t, unknown, "")
	_, admitted, err := unknown.AdmitParentAction(ParentActionResume)
	if err == nil || admitted {
		t.Fatalf("unknown reset resume = admitted:%v err:%v", admitted, err)
	}
	if !strings.Contains(err.Error(), "rate-limit reset evidence is missing") {
		t.Fatalf("unknown reset error = %v", err)
	}

	provider := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker", Role: WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(ResumeStopProviderUnavailable)
	checkpoint.ProviderUnavailableClassification = "http_5xx"
	checkpoint.ProviderUnavailableProbes = 1
	checkpoint.ProviderUnavailableStartedAt = time.Now()
	if err := provider.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetTaskStatus(TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
	_, admitted, err = provider.AdmitParentAction(ParentActionResume)
	if err != nil || !admitted {
		t.Fatalf("provider-unavailable resume = admitted:%v err:%v", admitted, err)
	}
}

func TestRejectResumeBeforeRateLimitResetPropagatesCheckpointReadError(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := os.WriteFile(st.Path(resumeStateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.rejectResumeBeforeRateLimitReset(); err == nil || !strings.Contains(err.Error(), "load rate-limit resume checkpoint") {
		t.Fatalf("checkpoint read error = %v", err)
	}
}

func writeRateLimitedWindowCheckpoint(t *testing.T, st *StateStore, resetAtRFC3339 string) {
	t.Helper()
	checkpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker", Role: WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(ResumeStopRateLimited)
	checkpoint.ResetAtRFC3339 = resetAtRFC3339
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
}
