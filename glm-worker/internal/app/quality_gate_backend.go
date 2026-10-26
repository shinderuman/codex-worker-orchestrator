package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func newQualityGateExecution(identity qualityGateStartIdentity, required string) (*qualitygate.Execution, *qualitygate.RunToolchain, error) {
	backend, err := qualitygate.ResolveBackend(identity.Repository, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, nil, err
	}
	host := qualitygate.CaptureToolchain(identity.WorkingDir, identity.GoArgs, qualityGateEnv(required))
	host.RequiredGoVersion = required
	if !host.MatchesRequiredGoVersion() {
		return nil, nil, fmt.Errorf("quality gate Go toolchain does not match required go%s: observed=%q", required, host.GoVersion)
	}
	if backend == qualitygate.BackendNative {
		return nil, host, nil
	}
	if host.GOOS != runtime.GOOS || host.GOARCH != runtime.GOARCH {
		return nil, nil, fmt.Errorf("host toolchain platform differs from the Docker policy")
	}
	module := filepath.Join(identity.Repository, "glm-worker")
	if identity.WorkingDir != module {
		return nil, nil, fmt.Errorf("正規Docker gateには宣言したglm-worker moduleが必要です")
	}
	plan := &qualitygate.Execution{Backend: backend, HostToolchain: host}
	pending := &qualitygate.RunToolchain{RequiredGoVersion: required, Command: host.Command, TestCache: "unknown"}
	return plan, pending, nil
}

func runQualityGateExecution(st *state.StateStore, record *qualitygate.RunRecord, args []string) ([]byte, error) {
	if record.Execution == nil {
		return runQualityGateProcess(*record, args)
	}
	if record.Execution.Backend != qualitygate.BackendDockerARM64 {
		return nil, fmt.Errorf("unsupported execution backend")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var primary bytes.Buffer
	toolchain, evidence, err := dockertest.RunCanonical(ctx, record.Repository, args, &primary, &primary)
	if toolchain != nil {
		record.Toolchain = toolchain
	}
	record.Execution.Docker = evidence
	if err != nil {
		return primary.Bytes(), err
	}
	if err := verifyQualityGateSource(*record); err != nil {
		return primary.Bytes(), err
	}
	err = runDarwinQualitySupplement(ctx, st, record)
	return primary.Bytes(), err
}

func runDarwinQualitySupplement(ctx context.Context, st *state.StateStore, record *qualitygate.RunRecord) error {
	args := qualitygate.DarwinSupplementArguments(record.Form)
	if len(args) == 0 {
		return fmt.Errorf("supplement requires a canonical Go test form")
	}
	required := record.Toolchain.RequiredGoVersion
	toolchain := qualitygate.CaptureToolchain(record.WorkingDir, args, qualityGateEnv(required))
	toolchain.RequiredGoVersion = required
	n := &qualitygate.NativeSupplement{Arguments: args, Toolchain: toolchain, Status: qualitygate.StatusRunning}
	record.Execution.NativeSupplement = n
	if !toolchain.MatchesRequiredGoVersion() || toolchain.GOOS != "darwin" || toolchain.GOARCH != "arm64" {
		return fmt.Errorf("Darwin補完toolchainを利用できません")
	}
	var log bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = record.WorkingDir, qualityGateEnv(required), &log, &log
	started := time.Now()
	runErr := cmd.Run()
	n.DurationMS = time.Since(started).Milliseconds()
	n.Status, n.ExitCode, n.ExitSource = qualityGateProcessOutcome(runErr)
	n.Toolchain.TestCache = qualitygate.TestCacheObservation(log.Bytes())
	path := st.Path(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID, qualitygate.NativeSupplementLog))
	writeErr := os.WriteFile(path, log.Bytes(), 0600)
	if writeErr == nil {
		n.Log = path
	}
	if runErr == nil && writeErr == nil {
		writeErr = qualitygate.VerifyDarwinCoverage(log.Bytes())
	}
	if writeErr != nil {
		n.Status = qualitygate.StatusFail
		if n.ExitCode == 0 {
			n.ExitCode, n.ExitSource = 1, state.ValidationExitSourceWrapper
		}
	}
	return errors.Join(runErr, writeErr)
}
