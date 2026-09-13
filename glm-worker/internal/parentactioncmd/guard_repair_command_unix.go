//go:build unix

package parentactioncmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type guardRepairCommandProcessGroup struct {
	pgid       int
	control    *os.File
	done       chan struct{}
	waitErr    error
	closeOnce  sync.Once
	controlErr error
}

func newGuardRepairCommandProcessGroup() (*guardRepairCommandProcessGroup, error) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create process-group owner pipe: %w", err)
	}
	anchor := exec.Command("sh", "-c", "trap '' HUP INT TERM; while IFS= read -r line; do :; done")
	anchor.Stdin = readEnd
	anchor.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := anchor.Start(); err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, fmt.Errorf("start process-group owner: %w", err)
	}
	if err := readEnd.Close(); err != nil {
		_ = writeEnd.Close()
		_ = anchor.Process.Kill()
		_ = anchor.Wait()
		return nil, fmt.Errorf("close process-group owner read pipe: %w", err)
	}
	group := &guardRepairCommandProcessGroup{
		pgid:    anchor.Process.Pid,
		control: writeEnd,
		done:    make(chan struct{}),
	}
	go func() {
		group.waitErr = anchor.Wait()
		close(group.done)
	}()
	return group, nil
}

func (g *guardRepairCommandProcessGroup) configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: g.pgid}
}

func guardRepairCommandSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

func (g *guardRepairCommandProcessGroup) signal(received os.Signal) error {
	if err := g.requireOwner(); err != nil {
		return err
	}
	signalValue, ok := received.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported guard repair process signal %T", received)
	}
	if err := syscall.Kill(-g.pgid, signalValue); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

func (g *guardRepairCommandProcessGroup) cancel() error {
	if err := g.requireOwner(); err != nil {
		return err
	}
	if err := syscall.Kill(-g.pgid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}

func (g *guardRepairCommandProcessGroup) requireOwner() error {
	select {
	case <-g.done:
		return fmt.Errorf("guard repair process-group owner exited unexpectedly: %w", g.waitErr)
	default:
		return nil
	}
}

func (g *guardRepairCommandProcessGroup) verifyGone(timeout time.Duration) error {
	g.closeControl()
	select {
	case <-g.done:
		return g.controlErr
	case <-time.After(timeout):
		return fmt.Errorf("guard repair process-group owner %d remained after timeout cancellation", g.pgid)
	}
}

func (g *guardRepairCommandProcessGroup) release(timeout time.Duration) error {
	g.closeControl()
	select {
	case <-g.done:
		if g.controlErr != nil {
			return g.controlErr
		}
		if g.waitErr != nil {
			return fmt.Errorf("guard repair process-group owner exit: %w", g.waitErr)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("guard repair process-group owner %d did not exit after release", g.pgid)
	}
}

func (g *guardRepairCommandProcessGroup) closeControl() {
	g.closeOnce.Do(func() {
		if err := g.control.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			g.controlErr = fmt.Errorf("close guard repair process-group owner pipe: %w", err)
		}
	})
}
