//go:build !unix

package runner

import (
	"os/exec"
	"time"
)

func newProcessGroupCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func terminateProcessGroup(pgid int, termGrace time.Duration) string {
	return "process group cleanup is unavailable on this platform"
}
