package controller

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type attemptRuntimeEvidence struct {
	store            *Store
	runtime          *state.StateStore
	workspace        string
	seal             *AttemptSeal
	taskID           string
	modelTranscripts []runtimeTranscriptWindowRecord
	parentRollouts   []runtimeTranscriptWindowRecord
	guardianRollouts []runtimeTranscriptWindowRecord
}

const attemptSealRuntimeEvidenceMissing = "runtime-evidence"

const (
	evidenceKindModelTranscript    = "model-transcript"
	evidenceKindParentTranscript   = "parent-transcript"
	evidenceKindGuardianTranscript = "guardian-transcript"
)

func (s *Store) captureAttemptRuntimeEvidence(source Admission, seal *AttemptSeal) (bool, error) {
	runtime, taskID, err := s.boundAttemptRuntime(source)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, nil
	}
	collector := attemptRuntimeEvidence{store: s, runtime: runtime, workspace: source.Workspace.Root, seal: seal, taskID: taskID}
	if err := collector.captureStateFiles(); err != nil {
		return false, err
	}
	if err := collector.captureInstructionSnapshots(); err != nil {
		return false, err
	}
	if err := collector.captureSessions(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *attemptRuntimeEvidence) captureInstructionSnapshots() error {
	for _, name := range evidenceExportInstructionFiles {
		path := filepath.Join(c.workspace, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
			continue
		}
		if err != nil {
			return err
		}
		if err := c.addFile(path, "instruction-snapshot", c.logicalName(evidenceExportSnapshotEntryDir+"/"+name)); err != nil {
			return err
		}
	}
	return nil
}

func applyAttemptSealRuntimeCoverage(seal *AttemptSeal, runtimeCaptured bool) {
	if runtimeCaptured {
		seal.Coverage = attemptSealCoverageComplete
		return
	}
	seal.Coverage = attemptSealCoverageIncomplete
	seal.Missing = append(seal.Missing, attemptSealRuntimeEvidenceMissing)
}

func (s *Store) boundAttemptRuntime(source Admission) (*state.StateStore, string, error) {
	cfg := s.config
	cfg.RepoHash = digestStrings(s.identity.LineageID, source.Attempt.SemanticTaskRef.TaskPath)
	runtime := state.AttachStateStore(cfg)
	info, err := os.Lstat(runtime.Path("."))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", s.requireNoModelRuntimeEvidence(source.Attempt.AttemptID)
	}
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("runtime evidence source is not a directory")
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", s.requireNoModelRuntimeEvidence(source.Attempt.AttemptID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("read runtime controller binding: %w", err)
	}
	if binding.AttemptID != source.Attempt.AttemptID {
		return nil, "", fmt.Errorf("runtime evidence belongs to another attempt")
	}
	if binding.TaskPath != source.Attempt.SemanticTaskRef.TaskPath || binding.TaskContractDigest != source.Attempt.SemanticTaskRef.ContractDigest {
		return nil, "", fmt.Errorf("runtime evidence semantic task binding is stale")
	}
	taskID, err := validateRuntimeEvidenceTask(runtime)
	return runtime, taskID, err
}

func validateRuntimeEvidenceTask(runtime *state.StateStore) (string, error) {
	taskID, err := runtime.TaskID()
	if err != nil {
		return "", err
	}
	if taskID == "" || filepath.Base(taskID) != taskID || strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("runtime evidence task identity is invalid")
	}
	return taskID, nil
}

func (c *attemptRuntimeEvidence) captureStateFiles() error {
	entries, err := os.ReadDir(c.runtime.Path("."))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "lock" || entry.IsDir() {
			continue
		}
		if err := c.addFile(c.runtime.Path(entry.Name()), "runtime-state", c.logicalName("state/"+entry.Name())); err != nil {
			return err
		}
	}
	files := []struct{ path, kind, logical string }{
		{c.runtime.ModelCallLogPath(c.taskID), "telemetry", c.logicalName("telemetry.jsonl")},
		{c.runtime.TaskEventLogPath(c.taskID), "task-events", c.logicalName("events.jsonl")},
		{c.runtime.TaskLiveStatusPath(c.taskID), "task-events", c.logicalName("live.json")},
		{c.runtime.RoundLogPath(c.taskID), "review-rounds", c.logicalName("rounds.jsonl")},
		{c.runtime.TaskLifecycleLogPath(c.taskID), "task-lifecycle", c.logicalName("lifecycle.jsonl")},
		{c.runtime.TaskAuthorityPathPath(c.taskID), "task-authority", c.logicalName("authority.path")},
		{c.runtime.TaskAuthorityContentPath(c.taskID), "task-authority", c.logicalName("authority.md")},
	}
	for _, file := range files {
		if _, err := os.Lstat(file.path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := c.addFile(file.path, file.kind, file.logical); err != nil {
			return err
		}
	}
	if err := c.captureValidationRuns(); err != nil {
		return err
	}
	return c.captureArtifactTree()
}

func (c *attemptRuntimeEvidence) captureValidationRuns() error {
	for _, directory := range []string{qualitygate.RunDirectory, evidenceExportValidationSmokeDirectory} {
		if err := c.captureRuntimeRunDirectory(directory); err != nil {
			return err
		}
	}
	return c.captureRepositoryValidationRuns()
}

func (c *attemptRuntimeEvidence) captureRuntimeRunDirectory(directory string) error {
	runs, err := os.ReadDir(c.runtime.Path(directory))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, entry := range runs {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		if err := c.captureRuntimeValidationRun(directory, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (c *attemptRuntimeEvidence) captureRuntimeValidationRun(directory, runID string) error {
	record, err := readValidationRunRecord(c.runtime, directory, runID)
	if err != nil || record == nil {
		return nil
	}
	if !validationRunBindsToWindow(*record, c.taskID, c.workspace, c.seal.StartedAt, c.seal.SealedAt) {
		return nil
	}
	root := c.runtime.Path(filepath.Join(directory, runID))
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return c.addFile(path, "runtime-state", c.logicalName("state/"+directory+"/"+runID+"/"+filepath.ToSlash(relative)))
	})
}

func (c *attemptRuntimeEvidence) captureRepositoryValidationRuns() error {
	module, err := c.store.repositoryValidationStore(c.workspace)
	if err != nil || module == nil {
		return err
	}
	for _, directory := range []string{qualitygate.RunDirectory, evidenceExportValidationSmokeDirectory} {
		if err := c.captureRepositoryRunDirectory(module, directory); err != nil {
			return err
		}
	}
	return nil
}

func (c *attemptRuntimeEvidence) captureRepositoryRunDirectory(module *state.StateStore, directory string) error {
	runs, err := os.ReadDir(module.Path(directory))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, entry := range runs {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		if err := c.captureRepositoryValidationRun(module, directory, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (c *attemptRuntimeEvidence) captureRepositoryValidationRun(module *state.StateStore, directory, runID string) error {
	record, err := readValidationRunRecord(module, directory, runID)
	if err != nil || record == nil {
		return nil
	}
	if !validationRunBindsToWindow(*record, c.taskID, c.workspace, c.seal.StartedAt, c.seal.SealedAt) {
		return nil
	}
	files := []string{qualitygate.RunFile, qualitygate.RunLog}
	if directory == evidenceExportValidationSmokeDirectory {
		files = []string{qualitygate.RunFile, evidenceExportValidationSmokeLog}
	}
	for _, file := range files {
		path := module.Path(filepath.Join(directory, runID, file))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		logical := c.logicalName("state/" + directory + "/" + runID + "/" + file)
		if c.sealHasEvidence(logical) {
			continue
		}
		if err := c.addFile(path, "runtime-state", logical); err != nil {
			return err
		}
	}
	return nil
}

func readValidationRunRecord(store *state.StateStore, directory, runID string) (*qualitygate.RunRecord, error) {
	if directory == qualitygate.RunDirectory {
		record, err := qualitygate.Read(store, runID)
		if err != nil {
			return nil, nil
		}
		return &record, nil
	}
	return readSmokeRunRecord(store, runID)
}

func (c *attemptRuntimeEvidence) sealHasEvidence(logical string) bool {
	for _, ref := range c.seal.EvidenceRefs {
		if ref.LogicalIdentity == logical {
			return true
		}
	}
	return false
}

func (c *attemptRuntimeEvidence) captureArtifactTree() error {
	root := c.runtime.ArtifactDir(c.taskID)
	if info, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("runtime artifact source is not a directory")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return c.addFile(path, "runtime-artifact", c.logicalName("artifacts/"+filepath.ToSlash(name)))
	})
}

func (c *attemptRuntimeEvidence) logicalName(suffix string) string {
	return c.seal.AttemptID + ":" + suffix
}

func (c *attemptRuntimeEvidence) addFile(path, kind, logical string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime evidence %s is not a regular file", logical)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	ref, err := c.store.PutEvidenceObject(kind, "application/octet-stream", logical, true, data)
	if err != nil {
		return err
	}
	c.seal.EvidenceRefs = append(c.seal.EvidenceRefs, ref)
	return nil
}

func (c *attemptRuntimeEvidence) addWindowedFile(path, kind, logical, sessionID, rolloutID string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime evidence %s is not a regular file", logical)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	window, windowed := scanTranscriptWindow(data, c.seal.StartedAt, c.seal.SealedAt, nil)
	if window.Unattributed != "" {
		return fmt.Errorf("required runtime evidence %s cannot be bounded to the attempt window: %s", logical, window.Unattributed)
	}
	ref, err := c.store.PutEvidenceObject(kind, "application/octet-stream", logical, true, windowed)
	if err != nil {
		return err
	}
	c.seal.EvidenceRefs = append(c.seal.EvidenceRefs, ref)
	record := runtimeTranscriptWindowRecord{
		SessionID: sessionID, RolloutID: rolloutID,
		TotalBytes: window.TotalBytes, StartOffset: window.StartOffset, EndOffset: window.EndOffset,
		RecordsBefore: window.RecordsBefore, RecordsAfter: window.RecordsAfter, Basis: window.Basis,
	}
	switch kind {
	case evidenceKindModelTranscript:
		c.modelTranscripts = append(c.modelTranscripts, record)
	case evidenceKindParentTranscript:
		c.parentRollouts = append(c.parentRollouts, record)
	case evidenceKindGuardianTranscript:
		c.guardianRollouts = append(c.guardianRollouts, record)
	}
	return nil
}

func (s *Store) requireNoModelRuntimeEvidence(attemptID string) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "mutations"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		identity, canonical, err := canonicalJSONRecordEntry(entry)
		if err != nil {
			return err
		}
		if !canonical {
			continue
		}
		var mutation MutationRecord
		if err := readJSON(s.mutationPath(identity), &mutation); err != nil {
			return err
		}
		if mutation.AttemptID == attemptID && strings.HasPrefix(mutation.Command, "model:") {
			return fmt.Errorf("required model runtime evidence is unavailable for attempt %s", attemptID)
		}
	}
	return nil
}
