package parentactioncmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	guardRepairCommandCellModeEnv = "GLM_GUARD_REPAIR_COMMAND_CELL_MODE"
	guardRepairGrandchildPIDEnv   = "GLM_GUARD_REPAIR_GRANDCHILD_PID_FILE"
	guardRepairGrandchildModeEnv  = "GLM_GUARD_REPAIR_GRANDCHILD"
)

func TestRunGuardRepairCommandTimesOut(t *testing.T) {
	t.Setenv(guardRepairCommandCellModeEnv, "sleep")
	started := time.Now()
	_, err := runGuardRepairCommandWithin(t.TempDir(), "guard repair Go tests", 500*time.Millisecond, os.Args[0], "-test.run=^TestGuardRepairCommandCell$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if !strings.Contains(err.Error(), "guard repair Go tests timed out after 500ms") {
		t.Fatalf("timeout diagnostic = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("timeout convergence took %s", elapsed)
	}
}

func TestRunGuardRepairCommandPreservesSuccess(t *testing.T) {
	t.Setenv(guardRepairCommandCellModeEnv, "success")
	output, err := runGuardRepairCommandWithin(t.TempDir(), "guard repair Go tests", 5*time.Second, os.Args[0], "-test.run=^TestGuardRepairCommandCell$")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "guard-repair-command-ok") {
		t.Fatalf("command output = %q", output)
	}
}

func TestGuardRepairCommandInterruptionKeepsRepairRetryable(t *testing.T) {
	_, st, record := newGuardRepairLifecycleState(t)
	record.Status = state.GuardRepairRunning
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	childErr := errors.New("child interrupted")
	cause := &guardRepairCommandInterruptedError{signal: os.Interrupt, err: childErr}
	if err := finishGuardRepairCandidateFailure(st, record, cause); !errors.Is(err, childErr) {
		t.Fatalf("interruption error = %v", err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairRunning {
		t.Fatalf("interrupted repair status = %s want %s", got.Status, state.GuardRepairRunning)
	}
}

func TestGuardRepairCommandTimeoutMarksRepairFailed(t *testing.T) {
	_, st, record := newGuardRepairLifecycleState(t)
	record.Status = state.GuardRepairRunning
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := finishGuardRepairCandidateFailure(st, record, context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairFailed {
		t.Fatalf("timed-out repair status = %s want %s", got.Status, state.GuardRepairFailed)
	}
}

func TestGuardRepairCommandCell(t *testing.T) {
	switch os.Getenv(guardRepairCommandCellModeEnv) {
	case "sleep":
		time.Sleep(30 * time.Second)
	case "success":
		if _, err := fmt.Fprintln(os.Stdout, "guard-repair-command-ok"); err != nil {
			t.Fatal(err)
		}
	case "grandchild":
		command := exec.Command(os.Args[0], "-test.run=^TestGuardRepairGrandchildCell$")
		command.Env = append(os.Environ(), guardRepairGrandchildModeEnv+"=1")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		pidFile := os.Getenv(guardRepairGrandchildPIDEnv)
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", command.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = command.Wait()
	}
}

func TestGuardRepairGrandchildCell(_ *testing.T) {
	if os.Getenv(guardRepairGrandchildModeEnv) != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func guardRepairGrandchildPIDFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "grandchild.pid")
}
