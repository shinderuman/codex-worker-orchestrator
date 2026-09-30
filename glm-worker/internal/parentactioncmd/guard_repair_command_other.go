//go:build !unix

package parentactioncmd

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

type guardRepairCommandProcessGroup struct{}

func newGuardRepairCommandProcessGroup() (*guardRepairCommandProcessGroup, error) {
	return nil, fmt.Errorf("guard repair process-tree ownership is unsupported on this platform")
}

func (g *guardRepairCommandProcessGroup) configure(_ *exec.Cmd) {}

func guardRepairCommandSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func (g *guardRepairCommandProcessGroup) signal(_ os.Signal) error {
	return fmt.Errorf("guard repair process-tree ownership is unsupported on this platform")
}

func (g *guardRepairCommandProcessGroup) cancel() error {
	return fmt.Errorf("guard repair process-tree ownership is unsupported on this platform")
}

func (g *guardRepairCommandProcessGroup) verifyGone(_ time.Duration) error {
	return fmt.Errorf("guard repair process-tree ownership is unsupported on this platform")
}

func (g *guardRepairCommandProcessGroup) release(_ time.Duration) error {
	return nil
}
