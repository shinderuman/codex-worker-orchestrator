package parentactioncmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

type guardRepairCommandInterruptedError struct {
	signal os.Signal
	err    error
}

const (
	guardRepairSubprocessTimeout = 10 * time.Minute
	guardRepairProcessSettleTime = 2 * time.Second
)

func (e *guardRepairCommandInterruptedError) Error() string {
	return fmt.Sprintf("guard repair subprocess interrupted by %s", e.signal)
}

func (e *guardRepairCommandInterruptedError) Unwrap() error {
	return e.err
}

func isGuardRepairCommandInterrupted(err error) bool {
	var interrupted *guardRepairCommandInterruptedError
	return errors.As(err, &interrupted)
}

func runGuardRepairCommand(dir, label, name string, args ...string) ([]byte, error) {
	return runGuardRepairCommandWithin(dir, label, guardRepairSubprocessTimeout, name, args...)
}

func runGuardRepairCommandWithin(dir, label string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	group, err := newGuardRepairCommandProcessGroup()
	if err != nil {
		return nil, fmt.Errorf("%s process-tree ownership unavailable: %w", label, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	group.configure(command)
	command.Dir = dir
	command.WaitDelay = guardRepairProcessSettleTime
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.Cancel = group.cancel

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, guardRepairCommandSignals()...)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		cleanupErr := group.release(guardRepairProcessSettleTime)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return output.Bytes(), guardRepairTimeoutError(label, timeout, ctxErr, output.String(), cleanupErr)
		}
		return output.Bytes(), errors.Join(err, cleanupErr)
	}
	return waitGuardRepairCommand(ctx, command, group, signals, label, timeout, &output)
}

func waitGuardRepairCommand(ctx context.Context, command *exec.Cmd, group *guardRepairCommandProcessGroup, signals <-chan os.Signal, label string, timeout time.Duration, output *bytes.Buffer) ([]byte, error) {
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	var interrupted os.Signal

	for {
		select {
		case received := <-signals:
			if interrupted == nil {
				interrupted = received
			}
			_ = group.signal(received)
		case err := <-waitDone:
			return guardRepairCommandWaitResult(ctx, group, interrupted, err, label, timeout, output)
		}
	}
}

func guardRepairCommandWaitResult(ctx context.Context, group *guardRepairCommandProcessGroup, interrupted os.Signal, waitErr error, label string, timeout time.Duration, output *bytes.Buffer) ([]byte, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		cleanupErr := group.verifyGone(guardRepairProcessSettleTime)
		return output.Bytes(), guardRepairTimeoutError(label, timeout, ctxErr, output.String(), cleanupErr)
	}
	cleanupErr := group.release(guardRepairProcessSettleTime)
	if interrupted != nil {
		return output.Bytes(), &guardRepairCommandInterruptedError{signal: interrupted, err: errors.Join(waitErr, cleanupErr)}
	}
	return output.Bytes(), errors.Join(waitErr, cleanupErr)
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
