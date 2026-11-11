package state

import (
	"strings"
	"testing"
	"time"
)

func TestScheduledPauseCheckpointRoundTripAndIsolation(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	want := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	checkpoint := ResumeCheckpoint{Stage: ResumeStageAutoFix, Phase: "worker-auto-fix-2", Role: WorkerRole, Model: "opus", ReportOnly: false}
	checkpoint.SetStopKind(ResumeStopScheduledPause)
	checkpoint.ScheduledResumeAt = want
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if got.StopKind != ResumeStopScheduledPause || got.ScheduledResumeAt != want || got.StopKind.TaskStatus() != TaskStatusScheduledPause ||
		got.StopKind.ResumeSource() != "scheduled-pause" || got.StopKind.ParentAction() != ParentActionResume {
		t.Fatalf("scheduled checkpoint roundtrip = %#v", got)
	}
	got.ClearStop()
	if got.ScheduledResumeAt != "" || got.StopKind != ResumeStopNone {
		t.Fatalf("clear stop leaked scheduling metadata: %#v", got)
	}
	checkpoint.SetStopKind(ResumeStopInterrupted)
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.ScheduledResumeAt != "" {
		t.Fatalf("scheduled metadata leaked into user interruption: %#v", checkpoint)
	}
}

func TestScheduledPauseCheckpointRejectsMalformedTime(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Model: "opus", StopKind: ResumeStopScheduledPause}
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "requires resume time") {
		t.Fatalf("missing boundary accepted: %v", err)
	}
	checkpoint.ScheduledResumeAt = "wrong-time"
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid boundary accepted: %v", err)
	}
	checkpoint.StopKind = ResumeStopInterrupted
	checkpoint.ScheduledResumeAt = time.Now().UTC().Format(time.RFC3339)
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "scheduled resume metadata") {
		t.Fatalf("cross-kind scheduled metadata accepted: %v", err)
	}
}
