//go:build !unix

package runner

import (
	"os/exec"
	"time"
)

// newProcessGroupCmdはprocess group非対応環境では通常のchild起動に縮退する。
func newProcessGroupCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// terminateProcessGroupはprocess group非対応環境ではgroup停止を強制できないため
// 残存warningを返す。childのwaitはcaller側で完了する。
func terminateProcessGroup(pgid int, termGrace time.Duration) string {
	return "process group cleanup is unavailable on this platform"
}
