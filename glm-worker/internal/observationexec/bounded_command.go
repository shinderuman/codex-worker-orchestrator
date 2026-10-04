package observationexec

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

type BoundedCommandResult struct {
	Stdout          []byte
	Stderr          []byte
	StdoutTotal     int64
	StderrTotal     int64
	StdoutTruncated bool
	StderrTruncated bool
	ExitCode        int
	ExitSource      string
	Err             error
}

const (
	BoundedCommandExitTarget    = "target"
	BoundedCommandExitDeadline  = "deadline"
	BoundedCommandExitCancelled = "cancelled"
	BoundedCommandExitWrapper   = "wrapper"
)

func RunBoundedCommand(ctx context.Context, workingDir, name string, args []string, deadline time.Duration, outputLimit int) BoundedCommandResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline <= 0 {
		return BoundedCommandResult{ExitCode: 1, ExitSource: BoundedCommandExitWrapper, Err: errors.New("bounded command requires a positive deadline")}
	}
	stdout := &boundedTailBuffer{limit: outputLimit}
	stderr := &boundedTailBuffer{limit: outputLimit}
	command := newObservationProcessGroupCmd(name, args...)
	command.Dir = workingDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return boundedCommandResult(stdout, stderr, 1, BoundedCommandExitWrapper, err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case err := <-waitDone:
		return boundedCommandResult(stdout, stderr, commandExitCode(err), BoundedCommandExitTarget, err)
	case <-timer.C:
		terminateObservationProcessGroup(command.Process.Pid)
		err := boundedCommandWait(waitDone)
		return boundedCommandResult(stdout, stderr, commandExitCode(err), BoundedCommandExitDeadline, err)
	case <-ctx.Done():
		terminateObservationProcessGroup(command.Process.Pid)
		err := boundedCommandWait(waitDone)
		if err == nil {
			err = ctx.Err()
		}
		return boundedCommandResult(stdout, stderr, commandExitCode(err), BoundedCommandExitCancelled, err)
	}
}

func boundedCommandWait(waitDone <-chan error) error {
	select {
	case err := <-waitDone:
		return err
	case <-time.After(deadlineGrace):
		return errors.New("bounded command did not reap after process-group termination")
	}
}

func boundedCommandResult(stdout, stderr *boundedTailBuffer, exitCode int, source string, err error) BoundedCommandResult {
	stdoutData, stdoutTotal, stdoutTruncated := stdout.snapshot()
	stderrData, stderrTotal, stderrTruncated := stderr.snapshot()
	return BoundedCommandResult{
		Stdout:          stdoutData,
		Stderr:          stderrData,
		StdoutTotal:     stdoutTotal,
		StderrTotal:     stderrTotal,
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
		ExitCode:        exitCode,
		ExitSource:      source,
		Err:             err,
	}
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return goTestExitCode(exitErr)
	}
	return 1
}
