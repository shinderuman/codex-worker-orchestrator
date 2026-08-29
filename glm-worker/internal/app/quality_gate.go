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

const (
	qualityGateLogDirectory = "quality-gate-logs"
	qualityGateRunDirectory = "quality-gate-runs"
	qualityGateRunFile      = "run.json"
	qualityGateRunLog       = "gate.log"
)

var qualityGateForms = map[string][]string{
	"go-test":      {"test", "./..."},
	"go-test-race": {"test", "-race", "./..."},
}

func (e *QualityGateError) Error() string {
	return fmt.Sprintf("quality gateが失敗しました (exit %d)", e.ExitCode)
}

func runQualityGate(payload string, st *state.StateStore, stdout io.Writer) error {
	if action, runID, ok := splitQualityGateAction(payload); ok {
		switch action {
		case "status":
			return printQualityGateRun(st, runID, false, stdout)
		case "watch":
			return printQualityGateRun(st, runID, true, stdout)
		case "result":
			return printQualityGateRun(st, runID, false, stdout)
		case "internal-run":
			return executeQualityGateRun(st, runID)
		}
	}
	return startQualityGate(payload, st, stdout)
}

func startQualityGate(form string, st *state.StateStore, stdout io.Writer) error {
	goArgs, ok := qualityGateForms[form]
	if !ok {
		return usageError("usage: glm-worker --quality-gate <go-test|go-test-race> | --quality-gate <status|watch|result> <validation-run-id>")
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("quality gateの作業dirを取得できません: %w", err)
	}
	repository, err := qualityGateRepositoryRoot(workingDir)
	if err != nil {
		return err
	}
	snapshot, err := state.CaptureGitSnapshot(repository)
	if err != nil {
		return fmt.Errorf("quality gate snapshotを取得できません: %w", err)
	}
	if err := os.MkdirAll(st.Path(qualityGateRunDirectory), 0o700); err != nil {
		return fmt.Errorf("quality gate run directoryを作成できません: %w", err)
	}
	lock, err := acquireQualityGateStartLock(st)
	if err != nil {
		return err
	}

	if existing, found := findRunningQualityGateRun(st, form, repository, snapshot); found {
		_ = lock.Close()
		_ = emitQualityGateStarted(existing.ValidationRunID, true)
		return printQualityGateRun(st, existing.ValidationRunID, true, stdout)
	}

	runID, err := newValidationRunID()
	if err != nil {
		_ = lock.Close()
		return err
	}
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            form,
		Repository:      repository,
		WorkingDir:      workingDir,
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          "running",
	}
	if err := writeQualityGateRun(st, record); err != nil {
		_ = lock.Close()
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		_ = lock.Close()
		return failQualityGateLaunch(st, record, fmt.Errorf("glm-worker executableを解決できません: %w", err))
	}
	child := exec.Command(executable, "--quality-gate", "internal-run", runID)
	child.Dir = workingDir
	child.Env = qualityGateEnv()
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		_ = lock.Close()
		return failQualityGateLaunch(st, record, fmt.Errorf("quality gate runnerを開始できません: %w", err))
	}
	_ = lock.Close()
	_ = emitQualityGateStarted(runID, false)

	waitErr := child.Wait()
	final, readErr := reconcileQualityGateRun(st, runID)
	if readErr != nil {
		return readErr
	}
	if final.Status == "running" && waitErr != nil {
		final.Status = "interrupted"
		final.ExitCode = -1
		completed := time.Now().UTC()
		final.CompletedAt = &completed
		final.DurationMS = completed.Sub(final.StartedAt).Milliseconds()
		if err := writeQualityGateRun(st, final); err != nil {
			return err
		}
	}
	return finishQualityGateCommand(final, goArgs, stdout)
}

func executeQualityGateRun(st *state.StateStore, runID string) error {
	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return err
	}
	if record.Status != "running" {
		return nil
	}
	goArgs, ok := qualityGateForms[record.Form]
	if !ok {
		return fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
	}
	record.RunnerPID = os.Getpid()
	if err := writeQualityGateRun(st, record); err != nil {
		return err
	}

	var gateLog bytes.Buffer
	gate := exec.Command("go", goArgs...)
	gate.Dir = record.WorkingDir
	gate.Env = qualityGateEnv()
	gate.Stdout = &gateLog
	gate.Stderr = &gateLog
	runErr := gate.Run()

	status := "pass"
	exitCode := 0
	if runErr != nil {
		status = "fail"
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				status = "interrupted"
			}
		} else {
			exitCode = 1
		}
	}
	logPath, err := writeQualityGateRunLog(st, runID, gateLog.Bytes())
	if err != nil {
		status = "fail"
		if exitCode == 0 {
			exitCode = 1
		}
		gateLog.WriteString("\nquality gate log persistence failed: " + err.Error() + "\n")
		logPath = ""
	}
	completed := time.Now().UTC()
	record.Status = status
	record.CompletedAt = &completed
	record.ExitCode = exitCode
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	record.Log = logPath
	if err := writeQualityGateRun(st, record); err != nil {
		return err
	}
	evidence := filepath.ToSlash(filepath.Join(qualityGateRunDirectory, runID, qualityGateRunLog))
	if logPath == "" {
		evidence = ""
	}
	st.RecordValidation("quality-gate", record.Form, "", status, exitCode, record.DurationMS, evidence)
	if status != "pass" {
		return qualityGateErrorFromRecord(record)
	}
	return nil
}

func finishQualityGateCommand(record qualityGateRunRecord, goArgs []string, stdout io.Writer) error {
	if record.Status != "pass" {
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
	goArgs := qualityGateForms[record.Form]
	return &QualityGateError{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(goArgs, " "),
		WorkingDir:      record.WorkingDir,
		ExitCode:        record.ExitCode,
		DurationMS:      record.DurationMS,
		LogPath:         record.Log,
	}
}

func printQualityGateRun(st *state.StateStore, runID string, watch bool, stdout io.Writer) error {
	for {
		record, err := reconcileQualityGateRun(st, runID)
		if err != nil {
			return err
		}
		if !watch || record.Status != "running" {
			return writeJSON(stdout, record)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func reconcileQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	if record.Status == "running" && record.RunnerPID > 0 && !qualityGateProcessAlive(record.RunnerPID) {
		completed := time.Now().UTC()
		record.Status = "interrupted"
		record.ExitCode = -1
		record.CompletedAt = &completed
		record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
		if err := writeQualityGateRun(st, record); err != nil {
			return qualityGateRunRecord{}, err
		}
	}
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
		if err != nil || record.Status != "running" {
			continue
		}
		if record.Form == form && record.Repository == repository && record.Head == snapshot.Head && record.IndexDigest == snapshot.IndexDigest && record.WorktreeDigest == snapshot.WorktreeDigest {
			return record, true
		}
	}
	return qualityGateRunRecord{}, false
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
	record.Status = "fail"
	record.ExitCode = 1
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	logPath, logErr := writeQualityGateRunLog(st, record.ValidationRunID, []byte(launchErr.Error()+"\n"))
	if logErr == nil {
		record.Log = logPath
	}
	if writeErr := writeQualityGateRun(st, record); writeErr != nil {
		return errors.Join(launchErr, writeErr)
	}
	return qualityGateErrorFromRecord(record)
}

func emitQualityGateStarted(runID string, attached bool) error {
	line, err := marshalEventLine(qualityGateStartedEvent{
		Type:            "control",
		Event:           "quality_gate_started",
		ValidationRunID: runID,
		Attached:        attached,
	})
	if err != nil {
		return err
	}
	_, err = os.Stderr.Write(line)
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
