package state

import (
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
	errorText := err.Error()
	if !strings.Contains(errorText, "cannot resume before the Z.ai 5h reset at") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(errorText, "automatic 5h recovery is machine-owned") || !strings.Contains(errorText, "explicit --resume is admitted after the reset boundary") {
		t.Fatalf("error does not describe canonical recovery: %v", err)
	}
	if strings.Contains(errorText, "--auto-resume-plan") {
		t.Fatalf("error references retired scheduler command: %v", err)
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

func TestAdmitParentActionFailsClosedWithoutRateLimitResetEvidence(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeRateLimitedWindowCheckpoint(t, st, "")

	_, admitted, err := st.AdmitParentAction(ParentActionResume)
	if err == nil || admitted {
		t.Fatalf("missing reset resume = admitted:%v err:%v", admitted, err)
	}
	if !strings.Contains(err.Error(), "rate-limit reset evidence is missing") {
		t.Fatalf("error = %v", err)
	}
	if status := st.TaskStatus(); status != TaskStatusRateLimited {
		t.Fatalf("missing reset rejection must keep the stopped task state, got %s", status)
	}
}

func TestAdmitParentActionRateLimitResetGateDoesNotApplyToOtherStops(t *testing.T) {
	provider := &StateStore{dir: t.TempDir()}
	providerCheckpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker", Role: WorkerRole, Model: "opus"}
	providerCheckpoint.SetStopKind(ResumeStopProviderUnavailable)
	providerCheckpoint.ProviderUnavailableClassification = "http_5xx"
	providerCheckpoint.ProviderUnavailableProbes = 1
	providerCheckpoint.ProviderUnavailableStartedAt = time.Now()
	if err := provider.SaveResumeCheckpoint(providerCheckpoint); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetTaskStatus(TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
	_, admitted, err := provider.AdmitParentAction(ParentActionResume)
	if err != nil || !admitted {
		t.Fatalf("provider-unavailable resume = admitted:%v err:%v", admitted, err)
	}

	interrupted := &StateStore{dir: t.TempDir()}
	interruptedCheckpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker", Role: WorkerRole, Model: "opus"}
	interruptedCheckpoint.SetStopKind(ResumeStopInterrupted)
	if err := interrupted.SaveResumeCheckpoint(interruptedCheckpoint); err != nil {
		t.Fatal(err)
	}
	if err := interrupted.SetTaskStatus(TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}
	_, admitted, err = interrupted.AdmitParentAction(ParentActionResume)
	if err != nil || !admitted {
		t.Fatalf("interrupted resume = admitted:%v err:%v", admitted, err)
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
