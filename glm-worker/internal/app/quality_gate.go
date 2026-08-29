package app

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type QualityGateError struct {
	ValidationRunID string
	Form            string
	Command         string
	WorkingDir      string
	ExitCode        int
	DurationMS      int64
	LogPath         string
}

type qualityGateOutput struct {
	Status          string `json:"status"`
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Command         string `json:"command"`
	WorkingDir      string `json:"working_dir"`
	DurationMS      int64  `json:"duration_ms"`
	Log             string `json:"log"`
}

type qualityGateRunRecord struct {
	ValidationRunID string     `json:"validation_run_id"`
	Form            string     `json:"form"`
	Repository      string     `json:"repository"`
	WorkingDir      string     `json:"working_dir"`
	Head            string     `json:"head"`
	IndexDigest     string     `json:"index_digest"`
	WorktreeDigest  string     `json:"worktree_digest"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Status          string     `json:"status"`
	RunnerPID       int        `json:"runner_pid,omitempty"`
	ExitCode        int        `json:"exit_code,omitempty"`
	DurationMS      int64      `json:"duration_ms,omitempty"`
	Log             string     `json:"log,omitempty"`
}

type qualityGateStartedEvent struct {
	Type            string `json:"type"`
	Event           string `json:"event"`
	ValidationRunID string `json:"validation_run_id"`
	Attached        bool   `json:"attached"`
}

type qualityGateRunnerWait func() error

type qualityGateRunnerLauncher func(*state.StateStore, qualityGateRunRecord) (qualityGateRunnerWait, error)

type qualityGateStartIdentity struct {
	Form       string
	GoArgs     []string
	Repository string
	WorkingDir string
	Snapshot   state.GitSnapshot
}

const (
	qualityGateRunDirectory       = "quality-gate-runs"
	qualityGateRunFile            = "run.json"
	qualityGateRunLog             = "gate.log"
	qualityGateStatusRunning      = "running"
	qualityGateStatusPass         = "pass"
	qualityGateStatusFail         = "fail"
	qualityGateStatusInterrupted  = "interrupted"
	qualityGateRunnerStartupGrace = 30 * time.Second
)

var qualityGateForms = map[string][]string{
	"go-test":      {"test", "./..."},
	"go-test-race": {"test", "-race", "./..."},
}

var launchQualityGateRunner qualityGateRunnerLauncher = launchQualityGateRunnerProcess

func (e *QualityGateError) Error() string {
	return fmt.Sprintf("quality gateが失敗しました (exit %d)", e.ExitCode)
}

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
		return usageError("%s", qualityGateCommandUsage)
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
	if existing, found := findRunningQualityGateRun(st, identity.Form, identity.Repository, identity.Snapshot); found {
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
	goArgs, ok := qualityGateForms[form]
	if !ok {
		return qualityGateStartIdentity{}, usageError("%s", qualityGateCommandUsage)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gateの作業dirを取得できません: %w", err)
	}
	repository, err := qualityGateRepositoryRoot(workingDir)
	if err != nil {
		return qualityGateStartIdentity{}, err
	}
	snapshot, err := state.CaptureGitSnapshot(repository)
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate snapshotを取得できません: %w", err)
	}
	if err := os.MkdirAll(st.Path(qualityGateRunDirectory), 0o700); err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate run directoryを作成できません: %w", err)
	}
	return qualityGateStartIdentity{
		Form:       form,
		GoArgs:     goArgs,
		Repository: repository,
		WorkingDir: workingDir,
		Snapshot:   snapshot,
	}, nil
}

func newQualityGateRunRecord(identity qualityGateStartIdentity) (qualityGateRunRecord, error) {
	runID, err := newValidationRunID()
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	return qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            identity.Form,
		Repository:      identity.Repository,
		WorkingDir:      identity.WorkingDir,
		Head:            identity.Snapshot.Head,
		IndexDigest:     identity.Snapshot.IndexDigest,
		WorktreeDigest:  identity.Snapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          qualityGateStatusRunning,
	}, nil
}

func launchQualityGateRunnerProcess(_ *state.StateStore, record qualityGateRunRecord) (qualityGateRunnerWait, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("glm-worker executableを解決できません: %w", err)
	}
	child := exec.Command(executable, "--quality-gate", qualityGateActionInternal, record.ValidationRunID)
	child.Dir = record.WorkingDir
	child.Env = qualityGateEnv()
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("quality gate runnerを開始できません: %w", err)
	}
	return child.Wait, nil
}

func executeQualityGateRun(st *state.StateStore, runID string) error {
	record, goArgs, active, err := beginQualityGateRun(st, runID)
	if err != nil || !active {
		return err
	}
	gateLog, runErr := runQualityGateProcess(record, goArgs)
	final, err := completeQualityGateRun(st, record, gateLog, runErr)
	if err != nil {
		return err
	}
	if final.Status != qualityGateStatusPass {
		return qualityGateErrorFromRecord(final)
	}
	return nil
}

func beginQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, []string, bool, error) {
	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualityGateRunRecord{}, nil, false, err
	}
	if record.Status != qualityGateStatusRunning {
		return record, nil, false, nil
	}
	goArgs, ok := qualityGateForms[record.Form]
	if !ok {
		return qualityGateRunRecord{}, nil, false, fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
	}
	record.RunnerPID = os.Getpid()
	if err := writeQualityGateRun(st, record); err != nil {
		return qualityGateRunRecord{}, nil, false, err
	}
	return record, goArgs, true, nil
}

func runQualityGateProcess(record qualityGateRunRecord, goArgs []string) ([]byte, error) {
	var gateLog bytes.Buffer
	gate := exec.Command("go", goArgs...)
	gate.Dir = record.WorkingDir
	gate.Env = qualityGateEnv()
	gate.Stdout = &gateLog
	gate.Stderr = &gateLog
	err := gate.Run()
	return gateLog.Bytes(), err
}

func completeQualityGateRun(st *state.StateStore, record qualityGateRunRecord, gateLog []byte, runErr error) (qualityGateRunRecord, error) {
	status, exitCode := qualityGateProcessOutcome(runErr)
	logPath, logErr := writeQualityGateRunLog(st, record.ValidationRunID, gateLog)
	if logErr != nil {
		status = qualityGateStatusFail
		if exitCode == 0 {
			exitCode = 1
		}
		logPath = ""
	}
	completed := time.Now().UTC()
	record.Status = status
	record.CompletedAt = &completed
	record.ExitCode = exitCode
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	record.Log = logPath
	if err := writeQualityGateRun(st, record); err != nil {
		return qualityGateRunRecord{}, err
	}
	recordQualityGateValidation(st, record)
	return record, nil
}

func qualityGateProcessOutcome(runErr error) (string, int) {
	if runErr == nil {
		return qualityGateStatusPass, 0
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return qualityGateStatusFail, 1
	}
	exitCode := exitErr.ExitCode()
	if exitCode < 0 {
		return qualityGateStatusInterrupted, exitCode
	}
	return qualityGateStatusFail, exitCode
}

func reconcileQualityGateAfterWait(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	final, err := reconcileQualityGateRun(st, runID)
	if err != nil || final.Status != qualityGateStatusRunning {
		return final, err
	}
	return markQualityGateInterrupted(st, final, "quality gate runner exited before persisting a terminal result")
}

func finishQualityGateCommand(record qualityGateRunRecord, goArgs []string, stdout io.Writer) error {
	if record.Status != qualityGateStatusPass {
		return qualityGateErrorFromRecord(record)
	}
	return writeJSON(stdout, qualityGateOutput{
		Status:          record.Status,
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(goArgs, " "),
		WorkingDir:      record.WorkingDir,
		DurationMS:      record.DurationMS,
		Log:             record.Log,
	})
}

func qualityGateErrorFromRecord(record qualityGateRunRecord) *QualityGateError {
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
	return writeJSON(stdout, record)
}

func waitQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	for {
		record, err := reconcileQualityGateRun(st, runID)
		if err != nil || record.Status != qualityGateStatusRunning {
			return record, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func reconcileQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	if record.Status != qualityGateStatusRunning {
		return record, nil
	}
	if record.RunnerPID > 0 {
		if !qualityGateProcessAlive(record.RunnerPID) {
			return markQualityGateInterrupted(st, record, "quality gate runner is no longer running")
		}
		return record, nil
	}
	if time.Since(record.StartedAt) >= qualityGateRunnerStartupGrace {
		return markQualityGateInterrupted(st, record, "quality gate runner did not publish its pid before startup grace elapsed")
	}
	return record, nil
}

func markQualityGateInterrupted(st *state.StateStore, record qualityGateRunRecord, reason string) (qualityGateRunRecord, error) {
	completed := time.Now().UTC()
	record.Status = qualityGateStatusInterrupted
	record.ExitCode = -1
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	if record.Log == "" {
		if logPath, err := writeQualityGateRunLog(st, record.ValidationRunID, []byte(reason+"\n")); err == nil {
			record.Log = logPath
		}
	}
	if err := writeQualityGateRun(st, record); err != nil {
		return qualityGateRunRecord{}, err
	}
	recordQualityGateValidation(st, record)
	return record, nil
}

func findRunningQualityGateRun(st *state.StateStore, form, repository string, snapshot state.GitSnapshot) (qualityGateRunRecord, bool) {
	entries, err := os.ReadDir(st.Path(qualityGateRunDirectory))
	if err != nil {
		return qualityGateRunRecord{}, false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validValidationRunID(entry.Name()) {
			continue
		}
		record, err := reconcileQualityGateRun(st, entry.Name())
		if err != nil || record.Status != qualityGateStatusRunning {
			continue
		}
		if sameQualityGateSnapshot(record, form, repository, snapshot) {
			return record, true
		}
	}
	return qualityGateRunRecord{}, false
}

func sameQualityGateSnapshot(record qualityGateRunRecord, form, repository string, snapshot state.GitSnapshot) bool {
	return record.Form == form &&
		record.Repository == repository &&
		record.Head == snapshot.Head &&
		record.IndexDigest == snapshot.IndexDigest &&
		record.WorktreeDigest == snapshot.WorktreeDigest
}

func acquireQualityGateStartLock(st *state.StateStore) (*RepoLock, error) {
	path := st.Path(filepath.Join(qualityGateRunDirectory, "start.lock"))
	for attempt := 0; attempt < 100; attempt++ {
		lock, err := AcquireRepoLock(path)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrRepoLockHeld) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, ErrRepoLockHeld
}

func qualityGateRepositoryRoot(workingDir string) (string, error) {
	out, err := exec.Command("git", "-C", workingDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("quality gate repository identityを取得できません: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func newValidationRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("validation run idを生成できません: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func validValidationRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil && strings.ToLower(runID) == runID
}

func qualityGateRunRelativePath(runID string) string {
	return filepath.Join(qualityGateRunDirectory, runID, qualityGateRunFile)
}

func writeQualityGateRun(st *state.StateStore, record qualityGateRunRecord) error {
	if !validValidationRunID(record.ValidationRunID) {
		return fmt.Errorf("invalid validation run id")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("quality gate run recordをencodeできません: %w", err)
	}
	if err := st.Write(qualityGateRunRelativePath(record.ValidationRunID), string(data)); err != nil {
		return fmt.Errorf("quality gate run recordを保存できません: %w", err)
	}
	return nil
}

func readQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	if !validValidationRunID(runID) {
		return qualityGateRunRecord{}, &NotFoundError{Message: "quality gate runが見つかりません"}
	}
	data, err := os.ReadFile(st.Path(qualityGateRunRelativePath(runID)))
	if errors.Is(err, os.ErrNotExist) {
		return qualityGateRunRecord{}, &NotFoundError{Message: "quality gate runが見つかりません"}
	}
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	var record qualityGateRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return qualityGateRunRecord{}, fmt.Errorf("quality gate run recordをdecodeできません: %w", err)
	}
	if record.ValidationRunID != runID {
		return qualityGateRunRecord{}, fmt.Errorf("quality gate run record identity mismatch")
	}
	return record, nil
}

func writeQualityGateRunLog(st *state.StateStore, runID string, data []byte) (string, error) {
	if !validValidationRunID(runID) {
		return "", fmt.Errorf("invalid validation run id")
	}
	path := st.Path(filepath.Join(qualityGateRunDirectory, runID, qualityGateRunLog))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func failQualityGateLaunch(st *state.StateStore, record qualityGateRunRecord, launchErr error) error {
	completed := time.Now().UTC()
	record.Status = qualityGateStatusFail
	record.ExitCode = 1
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	if logPath, err := writeQualityGateRunLog(st, record.ValidationRunID, []byte(launchErr.Error()+"\n")); err == nil {
		record.Log = logPath
	}
	if err := writeQualityGateRun(st, record); err != nil {
		return errors.Join(launchErr, err)
	}
	recordQualityGateValidation(st, record)
	return qualityGateErrorFromRecord(record)
}

func recordQualityGateValidation(st *state.StateStore, record qualityGateRunRecord) {
	evidence := ""
	if record.Log != "" {
		evidence = filepath.ToSlash(filepath.Join(qualityGateRunDirectory, record.ValidationRunID, qualityGateRunLog))
	}
	st.RecordValidation("quality-gate", record.Form, "", record.Status, record.ExitCode, record.DurationMS, evidence)
}

func emitQualityGateStarted(diagnostics io.Writer, runID string, attached bool) error {
	line, err := marshalEventLine(qualityGateStartedEvent{
		Type:            "control",
		Event:           "quality_gate_started",
		ValidationRunID: runID,
		Attached:        attached,
	})
	if err != nil {
		return err
	}
	_, err = diagnostics.Write(line)
	return err
}

func qualityGateProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func qualityGateEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOFLAGS=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOFLAGS=")
}
