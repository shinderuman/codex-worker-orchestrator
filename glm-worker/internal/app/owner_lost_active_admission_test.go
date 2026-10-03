package app

import (
	"strings"
	"testing"
)

func TestNewTaskAdmissionPreservesOwnerLostActiveTask(t *testing.T) {
	st := newParentAdmissionStore(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "preserve active task"); err != nil {
		t.Fatal(err)
	}

	err = admitParentCommand(Command{Mode: ModeNewTask}, st)
	if err == nil || !strings.Contains(err.Error(), "controller-semantic") {
		t.Fatalf("active new-task admission error = %v", err)
	}
	if got, err := st.TaskID(); err != nil || got != taskID {
		t.Fatalf("active task identity changed: got %q err=%v", got, err)
	}
	if got := st.ReadOr("last-request", ""); got != "preserve active task" {
		t.Fatalf("active task state changed: last-request = %q", got)
	}

}
