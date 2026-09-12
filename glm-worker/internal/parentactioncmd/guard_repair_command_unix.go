//go:build unix

package parentactioncmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureGuardRepairCommandProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func guardRepairCommandSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

func signalGuardRepairCommandProcess(pgid int, received os.Signal) error {
	signalValue, ok := received.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported guard repair process signal %T", received)
	}
	if err := syscall.Kill(-pgid, signalValue); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

func cancelGuardRepairCommandProcess(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

func verifyGuardRepairCommandProcessGone(pgid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(-pgid, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("guard repair process group %d remained after timeout cancellation", pgid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
