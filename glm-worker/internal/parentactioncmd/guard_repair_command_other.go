//go:build !unix

package parentactioncmd

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

func configureGuardRepairCommandProcess(_ *exec.Cmd) {}

func guardRepairCommandSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func signalGuardRepairCommandProcess(pid int, received os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(received)
}

func cancelGuardRepairCommandProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

func verifyGuardRepairCommandProcessGone(_ int, _ time.Duration) error {
	return nil
}
