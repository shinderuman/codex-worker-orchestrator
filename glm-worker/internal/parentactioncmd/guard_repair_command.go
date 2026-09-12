package parentactioncmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

const (
	guardRepairSubprocessTimeout = 10 * time.Minute
	guardRepairProcessSettleTime = 2 * time.Second
)

func runGuardRepairCommand(dir, label, name string, args ...string) ([]byte, error) {
	return runGuardRepairCommandWithin(dir, label, guardRepairSubprocessTimeout, name, args...)
}

func runGuardRepairCommandWithin(dir, label string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	configureGuardRepairCommandProcess(command)
	command.Dir = dir
	command.WaitDelay = guardRepairProcessSettleTime
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return cancelGuardRepairCommandProcess(command.Process.Pid)
	}

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, guardRepairCommandSignals()...)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return output.Bytes(), guardRepairTimeoutError(label, timeout, ctxErr, output.String(), nil)
		}
		return output.Bytes(), err
	}

	pid := command.Process.Pid
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	for {
		select {
		case received := <-signals:
			_ = signalGuardRepairCommandProcess(pid, received)
		case err := <-waitDone:
			if ctxErr := ctx.Err(); ctxErr != nil {
				cleanupErr := verifyGuardRepairCommandProcessGone(pid, guardRepairProcessSettleTime)
				return output.Bytes(), guardRepairTimeoutError(label, timeout, ctxErr, output.String(), cleanupErr)
			}
			return output.Bytes(), err
		}
	}
}

func guardRepairTimeoutError(label string, timeout time.Duration, cause error, output string, cleanupErr error) error {
	detail := compactGuardRepairCommandOutput(output)
	if cleanupErr != nil {
		if detail == "" {
			return fmt.Errorf("%s timed out after %s: %w; process cleanup failed: %w", label, timeout, cause, cleanupErr)
		}
		return fmt.Errorf("%s timed out after %s: %w; process cleanup failed: %w: %s", label, timeout, cause, cleanupErr, detail)
	}
	if detail == "" {
		return fmt.Errorf("%s timed out after %s: %w", label, timeout, cause)
	}
	return fmt.Errorf("%s timed out after %s: %w: %s", label, timeout, cause, detail)
}

func compactGuardRepairCommandOutput(output string) string {
	value := strings.Join(strings.Fields(output), " ")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
