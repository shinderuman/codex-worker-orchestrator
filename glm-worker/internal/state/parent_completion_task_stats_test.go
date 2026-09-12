package state

import (
	"os"
	"testing"
)

func TestCompleteParentAwaitingDoesNotRequireWritableTaskStatsMirror(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeEvent,
		Phase:              ParentPhaseAccept,
		Outcome:            ParentOutcomeAccepted,
		WorkerReportedRisk: "LOW",
	})
	if err := os.Mkdir(st.Path(currentStatsFile), 0o700); err != nil {
		t.Fatal(err)
	}

	evaluated := false
	completed, err := st.CompleteParentAwaiting(func(acceptedRisk string) (*SessionRotationEvaluation, error) {
		evaluated = true
		if acceptedRisk != "LOW" {
			t.Fatalf("accepted risk = %q", acceptedRisk)
		}
		return &SessionRotationEvaluation{
			ParentThreadID:           threadID,
			TaskID:                   taskID,
			Terminal:                 SessionRotationTerminalAccept,
			Decision:                 SessionRotationDecision{Evidence: []SessionRotationEvidence{}},
			AcceptedTasksUnavailable: true,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !completed || !evaluated {
		t.Fatalf("completion = completed:%v evaluated:%v", completed, evaluated)
	}
	if st.TaskStatus() != TaskStatusComplete {
		t.Fatalf("task status = %q", st.TaskStatus())
	}
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil || marker == nil || marker.LastEvaluation == nil || marker.LastEvaluation.TaskID != taskID {
		t.Fatalf("rotation marker = %#v err=%v", marker, err)
	}
	info, err := os.Stat(st.Path(currentStatsFile))
	if err != nil || !info.IsDir() {
		t.Fatalf("observational mirror fixture changed: info=%v err=%v", info, err)
	}
}
