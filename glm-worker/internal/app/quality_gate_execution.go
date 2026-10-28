package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func runQualityGate(payload string, st *state.StateStore, stdout io.Writer) error {
	return runQualityGateWithDiagnostics(payload, st, stdout, io.Discard)
}

func runQualityGateWithDiagnostics(payload string, st *state.StateStore, stdout, diagnostics io.Writer) error {
	if action, runID, ok := splitQualityGateAction(payload); ok {
		return runQualityGateAction(action, runID, st, stdout)
	}
	return startQualityGate(payload, st, stdout, diagnostics)
}

func runQualityGateAction(action, runID string, st *state.StateStore, stdout io.Writer) error {
	switch action {
	case qualityGateActionStatus, qualityGateActionResult:
		return printQualityGateRun(st, runID, false, stdout)
	case qualityGateActionWatch:
		return printQualityGateRun(st, runID, true, stdout)
	case qualityGateActionInternal:
		if err := executeQualityGateRun(st, runID); err != nil {
			return err
		}
		return printQualityGateRun(st, runID, false, stdout)
	default:
		return machinecli.UsageErrorf("%s", qualityGateCommandUsage)
	}
}

func startQualityGate(form string, st *state.StateStore, stdout, diagnostics io.Writer) error {
	identity, err := prepareQualityGateStart(form, st)
	if err != nil {
		return err
	}
	lock, err := acquireQualityGateStartLock(st)
	if err != nil {
		return err
	}
	if existing, found := findRunningQualityGateRun(st, identity.Form, identity.Repository, identity.WorkingDir, identity.Snapshot); found {
		_ = lock.Close()
		_ = emitQualityGateStarted(diagnostics, existing.ValidationRunID, true)
		final, err := waitQualityGateRun(st, existing.ValidationRunID)
		if err != nil {
			return err
		}
		return finishQualityGateCommand(final, qualityGateForms[final.Form], stdout)
	}
	record, err := newQualityGateRunRecord(identity)
	if err != nil {
		_ = lock.Close()
		return err
	}
	if err := writeQualityGateRun(st, record); err != nil {
		_ = lock.Close()
		return err
	}
	wait, err := launchQualityGateRunner(st, record)
	if err != nil {
		_ = lock.Close()
		return failQualityGateLaunch(st, record, err)
	}
	_ = lock.Close()
	_ = emitQualityGateStarted(diagnostics, record.ValidationRunID, false)
	_ = wait()
	final, err := reconcileQualityGateAfterWait(st, record.ValidationRunID)
	if err != nil {
		return err
	}
	return finishQualityGateCommand(final, identity.GoArgs, stdout)
}

func prepareQualityGateStart(form string, st *state.StateStore) (qualityGateStartIdentity, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gateの作業dirを取得できません: %w", err)
	}
	return prepareQualityGateStartAt(form, st, workingDir)
}

func prepareQualityGateStartAt(form string, st *state.StateStore, workingDir string) (qualityGateStartIdentity, error) {
	goArgs, ok := qualityGateForms[form]
	if !ok {
		return qualityGateStartIdentity{}, machinecli.UsageErrorf("%s", qualityGateCommandUsage)
	}
	workingDir, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gateの作業dirを解決できません: %w", err)
	}
	workingDir = filepath.Clean(workingDir)
	repository, err := qualityGateRepositoryRoot(workingDir)
	if err != nil {
		return qualityGateStartIdentity{}, err
	}
	snapshot, err := state.CaptureGitSnapshot(repository)
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate snapshotを取得できません: %w", err)
	}
	if err := os.MkdirAll(st.Path(qualitygate.RunDirectory), 0o700); err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate run directoryを作成できません: %w", err)
	}
	return qualityGateStartIdentity{
		Form:       form,
		GoArgs:     goArgs,
		Repository: repository,
		WorkingDir: workingDir,
		TaskID:     st.ReadOr("task.id", ""),
		Snapshot:   snapshot,
	}, nil
}

func newQualityGateRunRecord(identity qualityGateStartIdentity) (qualitygate.RunRecord, error) {
	absent, err := qualitygate.NoIgnoredInputs(identity.Repository)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	runID, err := qualitygate.NewRunID()
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	required, err := qualitygate.RequiredGoVersion(identity.Repository)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	execution, toolchain, err := newQualityGateExecution(identity, required)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	return qualitygate.RunRecord{
		Execution:                     execution,
		ValidationRunID:               runID,
		Form:                          identity.Form,
		Repository:                    identity.Repository,
		WorkingDir:                    identity.WorkingDir,
		Head:                          identity.Snapshot.Head,
		IndexDigest:                   identity.Snapshot.IndexDigest,
		WorktreeDigest:                identity.Snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: identity.Snapshot.WorktreeDigestExcludingParent,
		IgnoredInputsAbsent:           &absent,
		TaskID:                        identity.TaskID,
		Toolchain:                     toolchain,
		StartedAt:                     time.Now().UTC(),
		Status:                        qualitygate.StatusRunning,
	}, nil
}

func launchQualityGateRunnerProcess(_ *state.StateStore, record qualitygate.RunRecord) (qualityGateRunnerWait, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("glm-worker executableを解決できません: %w", err)
	}
	required := ""
	if record.Toolchain != nil {
		required = record.Toolchain.RequiredGoVersion
	}
	child := exec.Command(executable, "--quality-gate", qualityGateActionInternal, record.ValidationRunID)
	child.Dir = record.WorkingDir
	child.Env = qualityGateEnv(required)
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("quality gate runnerを開始できません: %w", err)
	}
	return child.Wait, nil
}

func defaultQualityGateResourceSampler(record qualitygate.RunRecord, publish func(qualitygate.ResourceSamples)) qualityGateResourceSampler {
	return qualitygate.StartResourceSampler(qualitygate.ResourceSamplerConfig{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		AsyncFirst:      true, Publish: publish,
	})
}

func executeQualityGateRun(st *state.StateStore, runID string) error {
	record, goArgs, active, err := beginQualityGateRun(st, runID)
	if err != nil || !active {
		return err
	}
	resourceRecord := record
	sampler := startQualityGateResourceSampler(resourceRecord, func(resources qualitygate.ResourceSamples) {
		_, _ = writeQualityGateResourceSamplesRecord(st, resourceRecord, &resources)
	})
	var gateLog []byte
	runErr := verifyQualityGateSource(record)
	if runErr == nil {
		runErr = verifyQualityGateToolchain(record, goArgs)
	}
	if runErr == nil {
		gateLog, runErr = runQualityGateExecution(st, &record, goArgs)
		if sourceErr := verifyQualityGateSource(record); sourceErr != nil {
			runErr = sourceErr
		}
	}
	if runErr != nil {
		gateLog = append(gateLog, []byte("\n"+runErr.Error()+"\n")...)
	}
	resources := sampler.Stop()
	final, err := completeQualityGateRun(st, record, gateLog, runErr, &resources)
	if err != nil {
		return err
	}
	if final.Status != qualitygate.StatusPass {
		return qualityGateErrorFromRecord(final)
	}
	return nil
}

func verifyQualityGateToolchain(record qualitygate.RunRecord, goArgs []string) error {
	if record.Execution != nil && record.Execution.Backend == qualitygate.BackendDockerARM64 {
		record.Toolchain = record.Execution.HostToolchain
	}
	if record.Toolchain == nil || record.Toolchain.RequiredGoVersion == "" {
		return fmt.Errorf("quality gate run %sが要求Go versionを記録していません", record.ValidationRunID)
	}
	observed := qualitygate.CaptureToolchain(record.WorkingDir, goArgs, qualityGateEnv(record.Toolchain.RequiredGoVersion))
	observed.RequiredGoVersion = record.Toolchain.RequiredGoVersion
	if !observed.MatchesRequiredGoVersion() {
		return fmt.Errorf("quality gate実行環境のGo toolchainが契約version go%sと一致しません: observed=%q", record.Toolchain.RequiredGoVersion, observed.GoVersion)
	}
	return nil
}

func verifyQualityGateSource(record qualitygate.RunRecord) error {
	snapshot, err := state.CaptureGitSnapshot(record.Repository)
	if err != nil {
		return fmt.Errorf("quality gate source snapshotを確認できません: %w", err)
	}
	if !sameQualityGateSnapshot(record, record.Form, record.Repository, record.WorkingDir, snapshot) {
		return fmt.Errorf("quality gate実行中にsource snapshotが変化しました")
	}
	if record.IgnoredInputsAbsent != nil && *record.IgnoredInputsAbsent {
		absent, err := qualitygate.NoIgnoredInputs(record.Repository)
		if err != nil {
			return err
		}
		if !absent {
			return fmt.Errorf("quality gate実行中にignored入力が追加されました")
		}
	}
	return nil
}

func beginQualityGateRun(st *state.StateStore, runID string) (qualitygate.RunRecord, []string, bool, error) {
	lock, err := acquireQualityGateRunStateLock(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, nil, false, err
	}
	defer func() { _ = lock.Close() }()

	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, nil, false, err
	}
	if record.Status != qualitygate.StatusRunning {
		return record, nil, false, nil
	}
	goArgs, ok := qualityGateForms[record.Form]
	if !ok {
		return qualitygate.RunRecord{}, nil, false, fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
	}
	record.RunnerPID = os.Getpid()
	if err := writeQualityGateRun(st, record); err != nil {
		return qualitygate.RunRecord{}, nil, false, err
	}
	return record, goArgs, true, nil
}

func runQualityGateProcess(record qualitygate.RunRecord, goArgs []string) ([]byte, error) {
	required := ""
	if record.Toolchain != nil {
		required = record.Toolchain.RequiredGoVersion
	}
	var gateLog bytes.Buffer
	gate := exec.Command("go", goArgs...)
	gate.Dir = record.WorkingDir
	gate.Env = qualityGateEnv(required)
	gate.Stdout = &gateLog
	gate.Stderr = &gateLog
	err := gate.Run()
	return gateLog.Bytes(), err
}

func completeQualityGateRun(st *state.StateStore, record qualitygate.RunRecord, gateLog []byte, runErr error, resources *qualitygate.ResourceSamples) (qualitygate.RunRecord, error) {
	lock, err := acquireQualityGateRunStateLock(st, record.ValidationRunID)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	gateLog, runErr = verifyCompletedQualityExecution(&record, gateLog, runErr)
	status, exitCode, exitSource := qualityGateProcessOutcome(runErr)
	timingsPath, timingsErr := writeQualityGateTestTimingsRecord(st, record, gateLog)
	if timingsErr != nil {
		gateLog = append(gateLog, []byte("\n"+timingsErr.Error()+"\n")...)
		status = qualitygate.StatusFail
		if exitCode == 0 {
			exitCode = 1
			exitSource = state.ValidationExitSourceWrapper
		}
		timingsPath = ""
	}
	resourcesPath, resourcesErr := writeQualityGateResourceSamplesRecord(st, record, resources)
	if resourcesErr != nil {
		record.ResourceSamplesError = resourcesErr.Error()
		gateLog = append(gateLog, []byte("\nresource samples: "+resourcesErr.Error()+"\n")...)
	}
	logPath, logErr := writeQualityGateRunLog(st, record.ValidationRunID, gateLog)
	if logErr != nil {
		status = qualitygate.StatusFail
		if exitCode == 0 {
			exitCode = 1
			exitSource = state.ValidationExitSourceWrapper
		}
		logPath = ""
	}
	record.TestTimings, record.ResourceSamples = timingsPath, resourcesPath
	if record.Toolchain != nil {
		record.Toolchain.TestCache = qualitygate.TestCacheObservation(gateLog)
	}
	completed := time.Now().UTC()
	record.Status = status
	record.CompletedAt = &completed
	record.ExitCode = exitCode
	record.ExitSource = exitSource
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	record.Log = logPath
	if err := writeQualityGateRun(st, record); err != nil {
		return qualitygate.RunRecord{}, err
	}
	recordQualityGateValidation(st, record)
	return record, nil
}

func qualityGateProcessOutcome(runErr error) (string, int, string) {
	if runErr == nil {
		return qualitygate.StatusPass, 0, state.ValidationExitSourceTarget
	}
	var target *qualitygate.TargetExitError
	if errors.As(runErr, &target) {
		return qualitygate.StatusFail, target.ExitCode(), state.ValidationExitSourceTarget
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return qualitygate.StatusFail, 1, state.ValidationExitSourceWrapper
	}
	exitCode := exitErr.ExitCode()
	if exitCode < 0 {
		return qualitygate.StatusInterrupted, exitCode, state.ValidationExitSourceUnknown
	}
	return qualitygate.StatusFail, exitCode, state.ValidationExitSourceTarget
}

func reconcileQualityGateAfterWait(st *state.StateStore, runID string) (qualitygate.RunRecord, error) {
	final, err := reconcileQualityGateRun(st, runID)
	if err != nil || final.Status != qualitygate.StatusRunning {
		return final, err
	}
	return markQualityGateInterrupted(st, runID, "quality gate runner exited before persisting a terminal result")
}

func finishQualityGateCommand(record qualitygate.RunRecord, goArgs []string, stdout io.Writer) error {
	if record.Status != qualitygate.StatusPass {
		return qualityGateErrorFromRecord(record)
	}
	return machinecli.WriteJSON(stdout, qualityGateOutput{
		Status:          record.Status,
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(goArgs, " "),
		WorkingDir:      record.WorkingDir,
		DurationMS:      record.DurationMS,
		Log:             record.Log,
	})
}

func qualityGateErrorFromRecord(record qualitygate.RunRecord) *QualityGateError {
	return &QualityGateError{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(qualityGateForms[record.Form], " "),
		WorkingDir:      record.WorkingDir,
		ExitCode:        record.ExitCode,
		DurationMS:      record.DurationMS,
		LogPath:         record.Log,
	}
}

func printQualityGateRun(st *state.StateStore, runID string, watch bool, stdout io.Writer) error {
	record, err := reconcileQualityGateRun(st, runID)
	if watch && err == nil {
		record, err = waitQualityGateRun(st, runID)
	}
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, record)
}

func waitQualityGateRun(st *state.StateStore, runID string) (qualitygate.RunRecord, error) {
	for {
		record, err := reconcileQualityGateRun(st, runID)
		if err != nil || record.Status != qualitygate.StatusRunning {
			return record, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func verifyCompletedQualityExecution(record *qualitygate.RunRecord, log []byte, runErr error) ([]byte, error) {
	if runErr != nil || record.Execution == nil {
		return log, runErr
	}
	if record.Toolchain != nil {
		record.Toolchain.TestCache = qualitygate.TestCacheObservation(log)
	}
	err := qualitygate.VerifyExecution(*record)
	if err != nil {
		log = append(log, []byte("\n"+err.Error()+"\n")...)
	}
	return log, err
}
