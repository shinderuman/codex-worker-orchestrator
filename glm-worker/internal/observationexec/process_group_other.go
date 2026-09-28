//go:build !unix

package observationexec

import (
	"os"
	"os/exec"
	"syscall"
)

func newObservationProcessGroupCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func terminateObservationProcessGroup(pgid int) {
	if process, err := os.FindProcess(pgid); err == nil {
		_ = process.Signal(syscall.SIGKILL)
	}
}
