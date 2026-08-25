//go:build unix

package runner

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func newProcessGroupCmd(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command
}

func terminateProcessGroup(pgid int, termGrace time.Duration) string {
	killGroup(pgid, syscall.SIGTERM)
	if !waitProcessGroupGone(pgid, termGrace) {
		killGroup(pgid, syscall.SIGKILL)
		waitProcessGroupGone(pgid, killSettleTimeout)
	}
	if signalProcessGroup(pgid, syscall.Signal(0)) == nil {
		return fmt.Sprintf("process group %dに残存processがあります", pgid)
	}
	return ""
}

func killGroup(pgid int, signal syscall.Signal) {
	_ = signalProcessGroup(pgid, signal)
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	return syscall.Kill(-pgid, signal)
}

func waitProcessGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if signalProcessGroup(pgid, syscall.Signal(0)) != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(processGroupPollGap)
	}
}
