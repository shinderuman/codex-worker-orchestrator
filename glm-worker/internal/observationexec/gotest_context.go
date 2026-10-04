package observationexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const exitSourceCancelled = "cancelled"

func RunIsolatedGoTestContext(ctx context.Context, input GoTestInput) GoTestOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateExecutionID(input.ExecutionID); err != nil {
		return goTestInputFailure(err)
	}
	if err := observationConfinementAdmission(); err != nil {
		return goTestConfinementFailure(err)
	}
	started := time.Now()
	tempRoot, err := os.MkdirTemp("", "glm-worker-observation-")
	if err != nil {
		return goTestWrapperFailure(started, fmt.Sprintf("隔離temp rootを作成できません: %v", err))
	}
	defer func() { _ = os.RemoveAll(tempRoot) }()
	if err := ConfinementPreflight(tempRoot); err != nil {
		outcome := goTestConfinementFailure(err)
		outcome.DurationMS = time.Since(started).Milliseconds()
		return outcome
	}
	if err := prepareIsolatedGoTestRoot(tempRoot, input.ModuleDir); err != nil {
		return goTestInputFailure(err)
	}
	run := runIsolatedGoTestProcessContext(ctx, input, tempRoot)
	run.outcome.DurationMS = time.Since(started).Milliseconds()
	return finalizeIsolatedGoTestOutcome(input, run)
}

func runIsolatedGoTestProcessContext(ctx context.Context, input GoTestInput, tempRoot string) isolatedGoTestRun {
	launchArgs, err := confinedLaunchArgs(tempRoot, append([]string{"go"}, isolatedGoTestArgs(input.Race)...))
	if err != nil {
		return isolatedGoTestRun{outcome: goTestConfinementFailure(err)}
	}
	env, err := isolatedGoTestEnv(tempRoot)
	if err != nil {
		return isolatedGoTestRun{outcome: goTestConfinementFailure(err)}
	}
	command := newObservationProcessGroupCmd(launchArgs[0], launchArgs[1:]...)
	command.Dir = filepath.Join(tempRoot, "input")
	command.Env = env
	gateLog := &boundedTailBuffer{limit: logMaxBytes}
	command.Stdout = gateLog
	command.Stderr = gateLog
	if err := command.Start(); err != nil {
		return capturedIsolatedGoTestRun(goTestStartFailure(err), gateLog)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	deadline := time.Duration(input.ResolvedGoTestDeadlineMS()) * time.Millisecond
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case runErr := <-waitDone:
		return capturedIsolatedGoTestRun(classifyIsolatedGoTestOutcome(runErr, time.Now().Add(time.Nanosecond), deadline), gateLog)
	case <-timer.C:
		terminateObservationProcessGroup(command.Process.Pid)
		return capturedIsolatedGoTestRun(goTestDeadlineOutcome(boundedGoTestWait(waitDone), deadline), gateLog)
	case <-ctx.Done():
		terminateObservationProcessGroup(command.Process.Pid)
		return capturedIsolatedGoTestRun(goTestCancelledOutcome(boundedGoTestWait(waitDone), ctx.Err()), gateLog)
	}
}

func boundedGoTestWait(waitDone <-chan error) error {
	select {
	case err := <-waitDone:
		return err
	case <-time.After(deadlineGrace):
		return context.DeadlineExceeded
	}
}

func goTestCancelledOutcome(runErr, cancelErr error) GoTestOutcome {
	exitCode := 1
	if runErr != nil {
		exitCode = goTestExitCode(runErr)
	}
	detail := "observation execution was cancelled; process group terminated"
	if cancelErr != nil {
		detail += ": " + cancelErr.Error()
	}
	return GoTestOutcome{Status: StatusFail, ExitCode: exitCode, ExitSource: exitSourceCancelled, Detail: detail}
}
