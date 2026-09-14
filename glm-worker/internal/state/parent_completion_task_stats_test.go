package state

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestCompleteParentAwaitingDoesNotRequireWritableTaskStatsMirror(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	seedParentCompletionOutcome(t, st, taskID, SessionRotationTerminalAccept, string(packet.RiskLow))
	if err := os.Remove(st.Path(currentStatsFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.Path(currentStatsFile), 0o700); err != nil {
		t.Fatal(err)
	}

	evaluated := false
	completed, err := st.CompleteParentAwaiting(func(acceptedRisk string) (*SessionRotationEvaluation, error) {
		evaluated = true
		if acceptedRisk != string(packet.RiskLow) {
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

func TestCompleteParentAwaitingRejectsInvalidCanonicalCompletionRisk(t *testing.T) {
	for _, risk := range []string{"", "MEDIUM"} {
		t.Run(risk, func(t *testing.T) {
			st := &StateStore{dir: t.TempDir()}
			taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
			seedParentCompletionOutcome(t, st, taskID, SessionRotationTerminalAccept, risk)

			evaluated := false
			completed, err := st.CompleteParentAwaiting(func(string) (*SessionRotationEvaluation, error) {
				evaluated = true
				return nil, nil
			})
			if err == nil {
				t.Fatal("invalid completion risk was accepted")
			}
			if completed || evaluated {
				t.Fatalf("completion = completed:%v evaluated:%v", completed, evaluated)
			}
			if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
				t.Fatalf("task status = %q", st.TaskStatus())
			}
		})
	}
}

func seedParentCompletionOutcome(t *testing.T, st *StateStore, taskID, terminal, risk string) {
	t.Helper()
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	if err := st.initializeParentReviewState(taskID); err != nil {
		t.Fatal(err)
	}
	state, err := st.loadParentReviewState()
	if err != nil {
		t.Fatal(err)
	}
	state.Completion = &ParentCompletionOutcome{Terminal: terminal, Risk: risk}
	if err := st.writeParentReviewState(state); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
}
