package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestNewTaskAdmissionRejectsOwnerLostActiveTaskUntilExplicitReset(t *testing.T) {
	st := newParentAdmissionStore(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "preserve active task"); err != nil {
		t.Fatal(err)
	}

	err = admitParentCommand(Command{Mode: ModeNewTask}, st)
	if err == nil || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("active new-task admission error = %v", err)
	}
	if got, err := st.TaskID(); err != nil || got != taskID {
		t.Fatalf("active task identity changed: got %q err=%v", got, err)
	}
	if got := st.ReadOr("last-request", ""); got != "preserve active task" {
		t.Fatalf("active task state changed: last-request = %q", got)
	}

	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}
	if got := st.TaskStatus(); got != state.TaskStatusNone {
		t.Fatalf("reset task status = %q", got)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("new task rejected after explicit reset: %v", err)
	}
}
