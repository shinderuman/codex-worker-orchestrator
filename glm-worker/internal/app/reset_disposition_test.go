package app

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResetDispositionCommandParsing(t *testing.T) {
	cmd, err := ParseCommand([]string{"--reset", "--disposition", "abandon"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeReset || cmd.Payload != "abandon" {
		t.Fatalf("parsed reset command = %#v", cmd)
	}
	if _, err := ParseCommand([]string{"--reset", "--disposition", "complete"}); err == nil {
		t.Fatal("unknown reset disposition was accepted")
	}
	if _, err := ParseCommand([]string{"--reset", "abandon"}); err == nil {
		t.Fatal("unscoped reset disposition syntax was accepted")
	}
}

func TestNewTaskAdmissionFailsClosedOnUnreadableResetDisposition(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetWithDisposition("abandon"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-disposition.json"), []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = admitParentCommand(Command{Mode: ModeNewTask}, st)
	if err == nil || !strings.Contains(err.Error(), "cannot verify reset disposition") {
		t.Fatalf("new task admitted with unreadable disposition: %v", err)
	}
}
