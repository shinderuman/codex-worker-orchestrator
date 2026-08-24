//go:build unix

package runner

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// newProcessGroupCmdはchildを独自process group(PGID=child PID)へ起動する構成を返す。
// 停止時にgroup全体へsignalを送るためで、親process groupへ混ぜない。
func newProcessGroupCmd(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command
}

// terminateProcessGroupは停止要求を受けたchild process groupを終了させる。
// まずgroupへTERMを送り、termGrace以内に全滅しない場合だけKILLへ昇格する。
// direct childのwaitはcallerのcommand.Wait()経由で完了済みであることを前提に、
// group非残存を確認して残存warningを返す。
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

// killGroupはprocess groupへsignalを送る。既に消滅している場合は何もしない。
func killGroup(pgid int, signal syscall.Signal) {
	_ = signalProcessGroup(pgid, signal)
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	return syscall.Kill(-pgid, signal)
}

// waitProcessGroupGoneはgroupが観測上消滅するまでpollingする。KILL直後の
// zombieがinitへ回収されるまでの短い窓を吸収するためのbest-effort確認である。
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
