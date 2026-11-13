package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type collectedValidationRun struct {
	run            qualitygate.RunRecord
	logEntry       string
	timingsEntry   string
	timings        *qualitygate.TestTimings
	resourcesEntry string
	resources      *qualitygate.ResourceSamples
	basis          string
	binding        string
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
	evidenceKindAcceptedCandidate    = "accepted-candidate"
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
		if state.ValidGeneratedUUID(record.TaskID) {
			archived, err := c.runtime.ArchivedTaskStatsEvidence(record.TaskID)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("verify historical validation task %s: %w", record.TaskID, err)
			}
			if err == nil && archived.Proven {
				return false, nil
			}
		}
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
	recordBytes, modified, err := readValidationRunFile(store, runID, qualitygate.RunFile)
	if err != nil {
		c.recordUnreadable(c.validationEntry("quality-gate/"+runID+"/run.json"), err.Error())
		return nil
	}
	record, err := qualitygate.Decode(recordBytes)
	if err != nil || record.ValidationRunID != runID {
		c.recordUnreadable(c.validationEntry("quality-gate/"+runID+"/run.json"), "validation run record is invalid or its run identity differs")
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
	c.commit(EvidenceExportEntry{
		Path:   c.validationEntry("quality-gate/" + runID + "/" + qualitygate.RunFile),
		Source: evidenceExportSourceGateRun, Basis: basis,
		SHA256: digestBytes(recordBytes), Bytes: int64(len(recordBytes)), CollectedAt: c.observedAt,
		Changing: modified.After(c.observedAt),
	}, recordBytes)
	if record.Form == smokeProbeForm {
		c.collectPostInstallProbe(store, record, runID, basis, storeBinding)
		return nil
	}
	logEntry := ""
	if c.addValidationLog(store.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.RunLog)), c.validationEntry("quality-gate/"+runID+"/"+qualitygate.RunLog), evidenceExportSourceGateRun, basis) {
		logEntry = c.validationEntry("quality-gate/" + runID + "/" + qualitygate.RunLog)
	}
	if record.Execution != nil && record.Execution.NativeSupplement != nil && record.Execution.NativeSupplement.Log != "" {
		c.addValidationLog(store.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.NativeSupplementLog)), c.validationEntry("quality-gate/"+runID+"/"+qualitygate.NativeSupplementLog), evidenceExportSourceGateRun, basis)
	}
	timingsEntry, timings := c.collectGateRunTimings(store, runID, record.Form, basis)
	resourcesEntry, resources := c.collectGateRunResources(store, runID, record.Form, basis)
	c.builder.validationRuns = append(c.builder.validationRuns, collectedValidationRun{
		run: record, logEntry: logEntry, timingsEntry: timingsEntry, timings: timings,
		resourcesEntry: resourcesEntry, resources: resources, basis: basis, binding: c.gateRunBinding(record, storeBinding),
	})
	return nil
}

func readValidationRunFile(store *state.StateStore, runID, filename string) ([]byte, time.Time, error) {
	return readValidationRunFileWithRead(store, runID, filename, func(file *os.File) ([]byte, error) {
		return io.ReadAll(file)
	})
}

func readValidationRunFileWithRead(store *state.StateStore, runID, filename string, read func(*os.File) ([]byte, error)) ([]byte, time.Time, error) {
	if !qualitygate.ValidRunID(runID) {
		return nil, time.Time{}, fmt.Errorf("invalid validation run ID")
	}
	if filename != qualitygate.RunFile && filename != "probe.log" {
		return nil, time.Time{}, fmt.Errorf("invalid validation run artifact")
	}
	return readValidationFileNoFollow(store.Path("."), runID, filename, read)
}

func (c *evidenceLiveCollector) collectPostInstallProbeLog(store *state.StateStore, record qualitygate.RunRecord, runID, basis string) string {
	entry := c.validationEntry("quality-gate/" + runID + "/probe.log")
	data, modified, err := readValidationRunFile(store, runID, "probe.log")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.recordMissing(entry)
		} else {
			c.recordUnreadable(entry, err.Error())
		}
		return ""
	}
	if record.LogSHA256 == "" || record.LogSHA256 != digestBytes(data) {
		c.recordUnreadable(entry, "post-install probe log digest is missing or differs from the run record")
		return ""
	}
	c.commit(EvidenceExportEntry{
		Path: entry, Source: evidenceExportSourceGateRun, Basis: basis,
		SHA256: digestBytes(data), Bytes: int64(len(data)), CollectedAt: c.observedAt,
		Changing: modified.After(c.observedAt),
	}, data)
	return entry
}

func (c *evidenceLiveCollector) collectPostInstallProbe(store *state.StateStore, record qualitygate.RunRecord, runID, basis, storeBinding string) {
	if err := c.verifyPostInstallProbeSource(store, record, runID); err != nil {
		c.recordUnreadable(c.validationEntry("quality-gate/"+runID+"/probe.log"), err.Error())
		return
	}
	logEntry := c.collectPostInstallProbeLog(store, record, runID, basis)
	c.builder.validationRuns = append(c.builder.validationRuns, collectedValidationRun{
		run: record, logEntry: logEntry, basis: basis, binding: c.gateRunBinding(record, storeBinding),
	})
}

func (c *evidenceLiveCollector) verifyPostInstallProbeSource(store *state.StateStore, record qualitygate.RunRecord, runID string) error {
	expected := store.Path(filepath.Join(qualitygate.RunDirectory, runID, "probe.log"))
	if record.TaskID != c.taskID || record.Log != expected {
		return fmt.Errorf("post-install probe runtime Task or log locator differs")
	}
	candidate, err := c.postInstallProbeCandidate(record)
	if err != nil {
		return err
	}
	return c.store.verifyExportedSmokeProbe(candidate, record)
}

func (c *evidenceLiveCollector) postInstallProbeCandidate(record qualitygate.RunRecord) (AcceptedCandidate, error) {
	refs, err := c.postInstallProbeLineage(record)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	for _, ref := range refs {
		if ref.Kind != evidenceKindAcceptedCandidate {
			continue
		}
		candidate, err := c.store.LoadAcceptedCandidate(ref)
		if err != nil {
			return AcceptedCandidate{}, err
		}
		if candidate.CommitOID != record.Head || candidate.TaskRef.ContractDigest != record.TaskAuthorityDigest {
			continue
		}
		for _, evidence := range candidate.Evidence {
			if evidence.Digest == record.InstallEvidenceDigest {
				return candidate, c.verifyPostInstallProbeRuntime(candidate)
			}
		}
	}
	return AcceptedCandidate{}, fmt.Errorf("post-install probe has no source-bound candidate in the canonical Task lineage")
}

func (c *evidenceLiveCollector) postInstallProbeLineage(record qualitygate.RunRecord) ([]EvidenceObjectRef, error) {
	if c.builder.head.EvidenceHeadRef == nil {
		return nil, fmt.Errorf("post-install probe has no canonical Task evidence authority")
	}
	head, err := c.store.LoadEvidenceHead(*c.builder.head.EvidenceHeadRef)
	if err != nil {
		return nil, err
	}
	task := c.attempt.SemanticTaskRef
	task.ContractDigest = record.TaskAuthorityDigest
	subject, ok := evidenceSubjectHead(head.TaskHeads, taskEvidenceSubjectID(task))
	if !ok {
		return nil, fmt.Errorf("post-install probe Task contract is absent from the canonical evidence authority")
	}
	revision, err := c.store.LoadTaskIndexRevision(subject.RevisionRef)
	if err != nil {
		return nil, err
	}
	return revision.PublicationLineageRecords, nil
}

func (c *evidenceLiveCollector) verifyPostInstallProbeRuntime(candidate AcceptedCandidate) error {
	attempt, err := c.store.loadAttempt(candidate.AttemptID)
	if err != nil {
		return err
	}
	taskID, err := c.store.attemptRuntimeTaskIdentity(c.builder.head, attempt)
	if err != nil {
		return err
	}
	if attempt.ExecutionAuthority == ExecutionAuthorityParentOnly {
		taskID = attempt.AttemptID
	}
	if taskID != c.taskID {
		return fmt.Errorf("post-install probe candidate belongs to a different runtime Task")
	}
	return nil
}

func (s *Store) verifyExportedSmokeProbe(candidate AcceptedCandidate, record qualitygate.RunRecord) error {
	workspace, err := s.smokeProbeWorkspace(candidate)
	if err != nil {
		return err
	}
	if record.Repository != workspace || record.WorkingDir != workspace {
		return fmt.Errorf("post-install probe checkout differs from the accepted candidate")
	}
	source, err := s.finalValidationSource(candidate)
	if err != nil {
		return err
	}
	deployment, err := s.smokeProbeDeployment(candidate)
	if err != nil {
		return err
	}
	for _, probe := range candidate.RequiredSmokeProbes {
		if smokeProbeMatchesSource(record, workspace, source, candidate, probe, deployment) {
			return nil
		}
	}
	return fmt.Errorf("post-install probe source, declaration, or deployment differs from the accepted candidate")
}

func (c *evidenceLiveCollector) collectGateRunTimings(store *state.StateStore, runID, form, basis string) (string, *qualitygate.TestTimings) {
	path := store.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.TestTimingsFile))
	entry := c.validationEntry("quality-gate/" + runID + "/" + qualitygate.TestTimingsFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	timings, err := readGateRunTimings(path, runID, form)
	if err != nil {
		c.recordUnreadable(entry, err.Error())
		return "", nil
	}
	if !c.addValidationLog(path, entry, evidenceExportSourceGateRun, basis) {
		return "", nil
	}
	return entry, timings
}

func readGateRunTimings(path, runID, form string) (*qualitygate.TestTimings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	timings, err := qualitygate.DecodeTestTimings(data)
	if err != nil {
		return nil, err
	}
	if timings.ValidationRunID != runID || timings.Form != form {
		return nil, fmt.Errorf("test timings run identity or form differs")
	}
	return &timings, nil
}

func (c *evidenceLiveCollector) collectGateRunResources(store *state.StateStore, runID, form, basis string) (string, *qualitygate.ResourceSamples) {
	path := store.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.ResourceSamplesFile))
	entry := c.validationEntry("quality-gate/" + runID + "/" + qualitygate.ResourceSamplesFile)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.recordMissing(entry)
		} else {
			c.recordUnreadable(entry, err.Error())
		}
		return "", nil
	}
	if !info.Mode().IsRegular() {
		c.recordUnreadable(entry, "source is not a regular file")
		return "", nil
	}
	resources, err := readGateRunResources(path, runID, form)
	if err != nil {
		c.recordUnreadable(entry, err.Error())
		return "", nil
	}
	if !c.addValidationLog(path, entry, evidenceExportSourceGateRun, basis) {
		return "", nil
	}
	return entry, resources
}

func readGateRunResources(path, runID, form string) (*qualitygate.ResourceSamples, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	resources, err := qualitygate.DecodeResourceSamples(data)
	if err != nil {
		return nil, err
	}
	if resources.ValidationRunID != runID || resources.Form != form {
		return nil, fmt.Errorf("resource samples run identity or form differs")
	}
	return &resources, nil
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
