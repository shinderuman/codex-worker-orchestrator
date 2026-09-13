//go:build unix

package parentactioncmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunGuardRepairCommandTimeoutKillsDescendants(t *testing.T) {
	pidFile := guardRepairGrandchildPIDFile(t)
	t.Setenv(guardRepairCommandCellModeEnv, "grandchild")
	t.Setenv(guardRepairGrandchildPIDEnv, pidFile)
	_, err := runGuardRepairCommandWithin(t.TempDir(), "guard repair Go tests", time.Second, os.Args[0], "-test.run=^TestGuardRepairCommandCell$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if strings.Contains(err.Error(), "process cleanup failed") {
		t.Fatalf("timeout left process-group cleanup failure: %v", err)
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("grandchild pid was not recorded before timeout: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(data))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("grandchild process %d remained after guard repair timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestGuardRepairProcessGroupOwnerSurvivesCommandExitUntilRelease(t *testing.T) {
	group, err := newGuardRepairCommandProcessGroup()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", "exit 0")
	group.configure(command)
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-group.done:
		t.Fatal("process-group owner exited before explicit release")
	default:
	}
	if err := group.release(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-group.done:
	default:
		t.Fatal("process-group owner remained after release")
	}
}

func guardRepairProcessTreeSupportedForTest() bool {
	return true
}
