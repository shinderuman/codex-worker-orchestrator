package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type collectedValidationRun struct {
	run      qualitygate.RunRecord
	logEntry string
	basis    string
	binding  string
}

const (
	evidenceValidationBindingTaskRecord      = "task-record"
	evidenceValidationBindingTaskStore       = "task-store-ownership"
	evidenceValidationBindingWorkspaceWindow = "workspace-window"
)

const (
	evidenceExportValidationSmokeDirectory = "install-smoke-runs"
	evidenceExportValidationSmokeLog       = "smoke.log"

	evidenceExportSourceGateRun      = "quality-gate-run"
	evidenceExportSourceInstallSmoke = "install-smoke-run"

	evidenceExportBasisRuntimeValidation   = "runtime-validation-store"
	evidenceExportBasisRepositoryWorkspace = "repository-workspace-window"
)

func (c *evidenceLiveCollector) collectValidationEvidence() error {
	if c.runtime != nil {
		if err := c.collectGateRuns(c.runtime, evidenceExportBasisRuntimeValidation, evidenceValidationBindingTaskStore, c.taskStoreGateRunBinds); err != nil {
			return err
		}
		if err := c.collectSmokeRuns(c.runtime); err != nil {
			return err
		}
	}
	module, err := c.repositoryValidationStore()
	if err != nil || module == nil {
		return err
	}
	return c.collectGateRuns(module, evidenceExportBasisRepositoryWorkspace, evidenceValidationBindingWorkspaceWindow, c.moduleGateRunBinds)
}

func (c *evidenceLiveCollector) taskStoreGateRunBinds(record qualitygate.RunRecord) (bool, error) {
	if record.TaskID != "" && record.TaskID != c.taskID {
		return false, fmt.Errorf("runtime validation store holds gate run %s bound to task %s, not %s",
			record.ValidationRunID, record.TaskID, c.taskID)
	}
	return true, nil
}

func (c *evidenceLiveCollector) moduleGateRunBinds(record qualitygate.RunRecord) (bool, error) {
	return c.gateRunBindsToAttempt(record), nil
}

func (c *evidenceLiveCollector) repositoryValidationStore() (*state.StateStore, error) {
	repoRoot, ok := c.attemptWorkspaceRoot()
	if !ok {
		return nil, nil
	}
	return c.store.repositoryValidationStore(repoRoot)
}

func (s *Store) repositoryValidationStore(workspaceRoot string) (*state.StateStore, error) {
	module := state.AttachStateStore(config.AppConfig{StateBase: s.config.StateBase, RepoHash: config.RepoHashFor(workspaceRoot)})
	root, err := module.Read("repo-root")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(root) != workspaceRoot {
		return nil, nil
	}
	return module, nil
}

func validationRunBindsToWindow(record qualitygate.RunRecord, runtimeTaskID, workspaceRoot string, windowStart, windowEnd time.Time) bool {
	if record.TaskID != "" {
		if record.TaskID != runtimeTaskID {
			return false
		}
	} else if !validationRunWithinRepositoryWorkspace(record, workspaceRoot) {
		return false
	}
	if record.StartedAt.After(windowEnd) {
		return false
	}
	return record.CompletedAt == nil || !record.CompletedAt.Before(windowStart)
}

func validationRunWithinRepositoryWorkspace(record qualitygate.RunRecord, workspaceRoot string) bool {
	if workspaceRoot == "" || record.Repository == "" || record.WorkingDir == "" || record.StartedAt.IsZero() {
		return false
	}
	if record.Repository != workspaceRoot {
		return false
	}
	relative, err := filepath.Rel(workspaceRoot, record.WorkingDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	return true
}

func (c *evidenceLiveCollector) collectGateRuns(store *state.StateStore, basis, storeBinding string, binds func(qualitygate.RunRecord) (bool, error)) error {
	runs, err := os.ReadDir(store.Path(qualitygate.RunDirectory))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.recordUnreadable(c.validationEntry(qualitygate.RunDirectory), err.Error())
		}
		return nil
	}
	for _, entry := range runs {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		if err := c.collectGateRun(store, entry.Name(), basis, storeBinding, binds); err != nil {
			return err
		}
	}
	return nil
}

func (c *evidenceLiveCollector) collectGateRun(store *state.StateStore, runID, basis, storeBinding string, binds func(qualitygate.RunRecord) (bool, error)) error {
	record, err := qualitygate.Read(store, runID)
	if err != nil {
		c.recordUnreadable(c.validationEntry("quality-gate/"+runID+"/run.json"), err.Error())
		return nil
	}
	if binds != nil {
		included, bindErr := binds(record)
		if bindErr != nil {
			return bindErr
		}
		if !included {
			return nil
		}
	}
	c.addFileBasis(store.Path(qualitygate.RunRelativePath(runID)), c.validationEntry("quality-gate/"+runID+"/"+qualitygate.RunFile), evidenceExportSourceGateRun, basis, false)
	logEntry := ""
	if c.addValidationLog(store.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.RunLog)), c.validationEntry("quality-gate/"+runID+"/"+qualitygate.RunLog), evidenceExportSourceGateRun, basis) {
		logEntry = c.validationEntry("quality-gate/" + runID + "/" + qualitygate.RunLog)
	}
	c.builder.validationRuns = append(c.builder.validationRuns, collectedValidationRun{
		run: record, logEntry: logEntry, basis: basis, binding: c.gateRunBinding(record, storeBinding),
	})
	return nil
}

func (c *evidenceLiveCollector) gateRunBinding(record qualitygate.RunRecord, storeBinding string) string {
	if record.TaskID == c.taskID {
		return evidenceValidationBindingTaskRecord
	}
	return storeBinding
}

func (c *evidenceLiveCollector) collectSmokeRuns(store *state.StateStore) error {
	runs, err := os.ReadDir(store.Path(evidenceExportValidationSmokeDirectory))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.recordUnreadable(c.validationEntry(evidenceExportValidationSmokeDirectory), err.Error())
		}
		return nil
	}
	for _, entry := range runs {
		runID := entry.Name()
		if !entry.IsDir() || !qualitygate.ValidRunID(runID) {
			continue
		}
		if err := c.collectSmokeRun(store, runID); err != nil {
			return err
		}
	}
	return nil
}

func (c *evidenceLiveCollector) collectSmokeRun(store *state.StateStore, runID string) error {
	logEntry := c.validationEntry("install-smoke/" + runID + "/" + evidenceExportValidationSmokeLog)
	logPath := store.Path(filepath.Join(evidenceExportValidationSmokeDirectory, runID, evidenceExportValidationSmokeLog))
	record, err := readSmokeRunRecord(store, runID)
	if err != nil {
		c.recordUnreadable(c.validationEntry("install-smoke/"+runID+"/"+qualitygate.RunFile), err.Error())
	}
	if record == nil {
		if _, statErr := os.Lstat(logPath); statErr == nil {
			c.addValidationLog(logPath, logEntry, evidenceExportSourceInstallSmoke, evidenceExportBasisRuntimeValidation)
		}
		return nil
	}
	if included, bindErr := c.taskStoreGateRunBinds(*record); bindErr != nil || !included {
		return bindErr
	}
	c.addFileBasis(store.Path(filepath.Join(evidenceExportValidationSmokeDirectory, runID, qualitygate.RunFile)),
		c.validationEntry("install-smoke/"+runID+"/"+qualitygate.RunFile), evidenceExportSourceInstallSmoke, evidenceExportBasisRuntimeValidation, false)
	logCollected := c.addValidationLog(logPath, logEntry, evidenceExportSourceInstallSmoke, evidenceExportBasisRuntimeValidation)
	collected := collectedValidationRun{run: *record, basis: evidenceExportBasisRuntimeValidation, binding: c.gateRunBinding(*record, evidenceValidationBindingTaskStore)}
	if logCollected {
		collected.logEntry = logEntry
	}
	c.builder.validationRuns = append(c.builder.validationRuns, collected)
	return nil
}

func readSmokeRunRecord(store *state.StateStore, runID string) (*qualitygate.RunRecord, error) {
	data, err := os.ReadFile(store.Path(filepath.Join(evidenceExportValidationSmokeDirectory, runID, qualitygate.RunFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record qualitygate.RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.ValidationRunID != runID {
		return nil, fmt.Errorf("install smoke run record identity mismatch")
	}
	return &record, nil
}

func (c *evidenceLiveCollector) addValidationLog(path, entryPath, source, basis string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.recordMissing(entryPath)
		} else {
			c.recordUnreadable(entryPath, err.Error())
		}
		return false
	}
	if !info.Mode().IsRegular() {
		c.recordUnreadable(entryPath, "source is not a regular file")
		return false
	}
	c.addFileBasis(path, entryPath, source, basis, false)
	return true
}

func (c *evidenceLiveCollector) gateRunBindsToAttempt(record qualitygate.RunRecord) bool {
	end := c.windowEnd
	if end.IsZero() {
		end = c.observedAt
	}
	workspaceRoot, _ := c.attemptWorkspaceRoot()
	return validationRunBindsToWindow(record, c.taskID, workspaceRoot, c.attempt.CreatedAt, end)
}

func (c *evidenceLiveCollector) validationEntry(suffix string) string {
	if c.prefix == evidenceExportRuntimeModeBound {
		return "bound/validation/" + suffix
	}
	return "live/validation/" + suffix
}
