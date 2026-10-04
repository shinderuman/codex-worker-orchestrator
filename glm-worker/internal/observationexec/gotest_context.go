package observationexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	deadline := time.Duration(input.ResolvedGoTestDeadlineMS()) * time.Millisecond
	executionCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
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
		if stopErr := executionCtx.Err(); stopErr != nil {
			outcome := goTestContextStopOutcome(stopErr, deadline)
			outcome.DurationMS = time.Since(started).Milliseconds()
			return outcome
		}
		return goTestInputFailure(err)
	}
	if stopErr := executionCtx.Err(); stopErr != nil {
		outcome := goTestContextStopOutcome(stopErr, deadline)
		outcome.DurationMS = time.Since(started).Milliseconds()
		return outcome
	}
	run := runIsolatedGoTestProcessContext(executionCtx, input, tempRoot, deadline)
	run.outcome.DurationMS = time.Since(started).Milliseconds()
	return finalizeIsolatedGoTestOutcome(input, run)
}

func runIsolatedGoTestProcessContext(ctx context.Context, input GoTestInput, tempRoot string, deadline time.Duration) isolatedGoTestRun {
	launchArgs, err := confinedLaunchArgs(tempRoot, append([]string{"go"}, isolatedGoTestArgs(input.Race)...))
	if err != nil {
		return isolatedGoTestRun{outcome: goTestConfinementFailure(err)}
	}
	env, err := isolatedGoTestEnvContext(ctx, tempRoot)
	if err != nil {
		if stopErr := ctx.Err(); stopErr != nil {
			return isolatedGoTestRun{outcome: goTestContextStopOutcome(stopErr, deadline)}
		}
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
	select {
	case runErr := <-waitDone:
		return capturedIsolatedGoTestRun(classifyIsolatedGoTestOutcome(runErr, time.Now().Add(time.Nanosecond), deadline), gateLog)
	case <-ctx.Done():
		terminateObservationProcessGroup(command.Process.Pid)
		return capturedIsolatedGoTestRun(goTestContextStopOutcomeWithRunErr(boundedGoTestWait(waitDone), ctx.Err(), deadline), gateLog)
	}
}

func isolatedGoTestEnvContext(ctx context.Context, tempRoot string) ([]string, error) {
	allowed := []string{"PATH", "TZ", "LANG", "LC_ALL", "LC_CTYPE"}
	env := make([]string, 0, len(allowed)+10)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		for _, candidate := range allowed {
			if name == candidate {
				env = append(env, entry)
			}
		}
	}
	moduleCache, err := isolatedGoModuleCacheContext(ctx)
	if err != nil {
		return nil, err
	}
	return append(env,
		"HOME="+filepath.Join(tempRoot, "home"),
		"TMPDIR="+filepath.Join(tempRoot, "tmp"),
		"GOTMPDIR="+filepath.Join(tempRoot, "tmp"),
		"GOCACHE="+filepath.Join(tempRoot, "cache"),
		"GOMODCACHE="+moduleCache,
		"GOFLAGS=-mod=readonly",
		"GOPROXY=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
	), nil
}

func isolatedGoModuleCacheContext(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, "go", "env", "GOMODCACHE")
	command.Env = append(os.Environ(), "GOENV=off")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("隔離go testのmodule cacheを解決できません: %w", err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("隔離go testのmodule cacheがabsolute pathではありません: %q", path)
	}
	return filepath.Clean(path), nil
}

func boundedGoTestWait(waitDone <-chan error) error {
	select {
	case err := <-waitDone:
		return err
	case <-time.After(deadlineGrace):
		return context.DeadlineExceeded
	}
}

func goTestContextStopOutcome(err error, deadline time.Duration) GoTestOutcome {
	return goTestContextStopOutcomeWithRunErr(nil, err, deadline)
}

func goTestContextStopOutcomeWithRunErr(runErr, stopErr error, deadline time.Duration) GoTestOutcome {
	if errors.Is(stopErr, context.DeadlineExceeded) {
		return goTestDeadlineOutcome(runErr, deadline)
	}
	return goTestCancelledOutcome(runErr, stopErr)
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
