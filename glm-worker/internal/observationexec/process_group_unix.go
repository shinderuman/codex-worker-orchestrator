//go:build unix

package observationexec

import (
	"os/exec"
	"syscall"
	"time"
)

const processGroupPollGap = 50 * time.Millisecond

func newObservationProcessGroupCmd(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command
}

func terminateObservationProcessGroup(pgid int) {
	_ = signalObservationProcessGroup(pgid, syscall.SIGTERM)
	if waitObservationProcessGroupGone(pgid, deadlineGrace) {
		return
	}
	_ = signalObservationProcessGroup(pgid, syscall.SIGKILL)
	waitObservationProcessGroupGone(pgid, deadlineGrace)
}

func signalObservationProcessGroup(pgid int, signal syscall.Signal) error {
	return syscall.Kill(-pgid, signal)
}

func waitObservationProcessGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if signalObservationProcessGroup(pgid, syscall.Signal(0)) != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(processGroupPollGap)
	}
}
