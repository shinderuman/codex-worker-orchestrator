package parentactioncmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func beginGuardRepairIntegration(
	st *state.StateStore,
	record state.GuardRepairRecord,
	origin guardRepairOrigin,
	repoRoot string,
	repairRoot string,
	changed []string,
) (state.GuardRepairIntegrationJournal, error) {
	if _, err := st.LoadGuardRepairIntegrationJournal(); err == nil {
		return state.GuardRepairIntegrationJournal{}, fmt.Errorf("unfinished guard repair integration journal already exists")
	} else if !errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		return state.GuardRepairIntegrationJournal{}, err
	}
	preimages, err := captureGuardRepairFiles(repoRoot, changed)
	if err != nil {
		return state.GuardRepairIntegrationJournal{}, err
	}
	postimages, err := captureGuardRepairFiles(repairRoot, changed)
	if err != nil {
		return state.GuardRepairIntegrationJournal{}, err
	}
	files, err := guardRepairIntegrationJournalFiles(preimages, postimages)
	if err != nil {
		return state.GuardRepairIntegrationJournal{}, err
	}
	journal := state.GuardRepairIntegrationJournal{
		TaskID:             record.TaskID,
		Phase:              record.Phase,
		Fingerprint:        record.Fingerprint,
		Strategy:           record.Strategy,
		RelevantDigest:     record.RelevantDigest,
		ResumeCheckpoint:   origin.checkpoint,
		RepositoryBoundary: origin.boundary,
		Files:              files,
	}
	if err := st.SaveGuardRepairIntegrationJournal(journal); err != nil {
		return state.GuardRepairIntegrationJournal{}, err
	}
	return journal, nil
}

func guardRepairIntegrationJournalFiles(preimages, postimages []guardRepairFileBackup) ([]state.GuardRepairIntegrationFile, error) {
	if len(preimages) != len(postimages) {
		return nil, fmt.Errorf("guard repair integration image sets differ")
	}
	files := make([]state.GuardRepairIntegrationFile, 0, len(preimages))
	for i, preimage := range preimages {
		postimage := postimages[i]
		if preimage.path != postimage.path {
			return nil, fmt.Errorf("guard repair integration image path mismatch: %s != %s", preimage.path, postimage.path)
		}
		files = append(files, state.GuardRepairIntegrationFile{
			Path:        preimage.path,
			Content:     append([]byte(nil), preimage.content...),
			Mode:        uint32(preimage.mode.Perm()),
			Exists:      preimage.exists,
			PostContent: append([]byte(nil), postimage.content...),
			PostMode:    uint32(postimage.mode.Perm()),
			PostExists:  postimage.exists,
		})
	}
	return files, nil
}

func persistReadyGuardRepairIntegration(st *state.StateStore, record state.GuardRepairRecord) error {
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return err
	}
	return st.RemoveGuardRepairIntegrationJournal()
}

func recoverGuardRepairIntegrationIfNeeded(cfg config.AppConfig, st *state.StateStore) error {
	if _, err := st.LoadGuardRepairIntegrationJournal(); errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := repolock.AcquireWait(st.LockPath())
	if err != nil {
		return err
	}
	journal, err := st.LoadGuardRepairIntegrationJournal()
	if errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		return lock.Close()
	}
	if err != nil {
		return errors.Join(err, lock.Close())
	}
	return errors.Join(rollbackGuardRepairIntegration(cfg, st, journal), lock.Close())
}

func rollbackGuardRepairIntegration(cfg config.AppConfig, st *state.StateStore, journal state.GuardRepairIntegrationJournal) error {
	record, err := validateGuardRepairIntegrationRecovery(cfg, st, journal)
	if err != nil {
		return err
	}
	backups := make([]guardRepairFileBackup, 0, len(journal.Files))
	for _, file := range journal.Files {
		backups = append(backups, guardRepairFileBackup{
			path:    file.Path,
			content: append([]byte(nil), file.Content...),
			mode:    os.FileMode(file.Mode),
			exists:  file.Exists,
		})
	}
	if err := restoreGuardRepairFiles(cfg.RepoRoot, backups); err != nil {
		return fmt.Errorf("restore interrupted guard repair source: %w", err)
	}
	if err := st.SaveResumeCheckpoint(journal.ResumeCheckpoint); err != nil {
		return fmt.Errorf("restore interrupted guard repair checkpoint: %w", err)
	}
	restored, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("verify interrupted guard repair rollback: %w", err)
	}
	if !sameRepositoryBoundary(journal.RepositoryBoundary, restored) {
		return fmt.Errorf("interrupted guard repair rollback did not restore original repository boundary")
	}
	record.Status = state.GuardRepairRequested
	record.RepairedDigest = ""
	record.OriginalResumeObserved = false
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return fmt.Errorf("reset guard repair record after interrupted integration: %w", err)
	}
	if err := st.RemoveGuardRepairIntegrationJournal(); err != nil {
		return err
	}
	return nil
}

func validateGuardRepairIntegrationRecovery(
	cfg config.AppConfig,
	st *state.StateStore,
	journal state.GuardRepairIntegrationJournal,
) (state.GuardRepairRecord, error) {
	if err := validateGuardRepairIntegrationTask(st, journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	record, err := validateGuardRepairIntegrationRecord(st, journal)
	if err != nil {
		return state.GuardRepairRecord{}, err
	}
	if err := validateGuardRepairIntegrationCheckpoint(st, journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	if err := validateGuardRepairIntegrationPaths(journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	if err := validateGuardRepairIntegrationAuthority(cfg, journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	if err := validateGuardRepairIntegrationFiles(cfg.RepoRoot, journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	if err := validateGuardRepairIntegrationDirty(cfg, journal); err != nil {
		return state.GuardRepairRecord{}, err
	}
	return record, nil
}

func validateGuardRepairIntegrationTask(st *state.StateStore, journal state.GuardRepairIntegrationJournal) error {
	if st.ReadOr("task.id", "") != journal.TaskID || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return fmt.Errorf("guard repair integration journal belongs to a stale or foreign task")
	}
	return nil
}

func validateGuardRepairIntegrationRecord(st *state.StateStore, journal state.GuardRepairIntegrationJournal) (state.GuardRepairRecord, error) {
	record, err := st.LoadGuardRepairRecord()
	if err != nil {
		return state.GuardRepairRecord{}, fmt.Errorf("verify guard repair integration record: %w", err)
	}
	if record.TaskID != journal.TaskID || record.Phase != journal.Phase || record.Fingerprint != journal.Fingerprint ||
		record.Strategy != journal.Strategy || record.RelevantDigest != journal.RelevantDigest {
		return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal provenance does not match current repair record")
	}
	switch record.Status {
	case state.GuardRepairRequested, state.GuardRepairRunning, state.GuardRepairReady, state.GuardRepairFailed:
		return record, nil
	default:
		return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal cannot recover from record status %q", record.Status)
	}
}

func validateGuardRepairIntegrationCheckpoint(st *state.StateStore, journal state.GuardRepairIntegrationJournal) error {
	checkpoint, err := st.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify guard repair integration checkpoint: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.Phase != journal.Phase {
		return fmt.Errorf("guard repair integration journal checkpoint provenance does not match current checkpoint")
	}
	return nil
}

func validateGuardRepairIntegrationPaths(journal state.GuardRepairIntegrationJournal) error {
	for _, file := range journal.Files {
		if !guardrepair.IsAllowed(file.Path) {
			return fmt.Errorf("guard repair integration journal contains out-of-scope path %s", file.Path)
		}
	}
	return nil
}

func validateGuardRepairIntegrationAuthority(cfg config.AppConfig, journal state.GuardRepairIntegrationJournal) error {
	current, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if !sameGuardRepairRecoveryAuthority(journal.RepositoryBoundary, current) {
		return fmt.Errorf("repository authority changed after guard repair integration journal was created")
	}
	return nil
}

func validateGuardRepairIntegrationFiles(repoRoot string, journal state.GuardRepairIntegrationJournal) error {
	paths := make([]string, 0, len(journal.Files))
	for _, file := range journal.Files {
		paths = append(paths, file.Path)
	}
	current, err := captureGuardRepairFiles(repoRoot, paths)
	if err != nil {
		return fmt.Errorf("verify interrupted guard repair files: %w", err)
	}
	for i, file := range journal.Files {
		if !guardRepairBackupMatchesImage(current[i], file.Content, file.Mode, file.Exists) &&
			!guardRepairBackupMatchesImage(current[i], file.PostContent, file.PostMode, file.PostExists) {
			return fmt.Errorf("interrupted guard repair path changed after integration stopped: %s", file.Path)
		}
	}
	return nil
}

func guardRepairBackupMatchesImage(current guardRepairFileBackup, content []byte, mode uint32, exists bool) bool {
	if current.exists != exists {
		return false
	}
	if !exists {
		return true
	}
	return current.mode.Perm() == os.FileMode(mode).Perm() && bytes.Equal(current.content, content)
}

func validateGuardRepairIntegrationDirty(cfg config.AppConfig, journal state.GuardRepairIntegrationJournal) error {
	currentDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return err
	}
	paths := make(map[string]struct{}, len(journal.Files))
	for _, file := range journal.Files {
		paths[file.Path] = struct{}{}
	}
	if diff := state.DescribeStopDirtyDiff(
		filterGuardRepairIntegrationDirty(journal.ResumeCheckpoint.StopDirtyFiles, paths),
		filterGuardRepairIntegrationDirty(currentDirty, paths),
	); diff != "" {
		return fmt.Errorf("repository source outside interrupted guard repair changed: %s", diff)
	}
	return nil
}

func sameGuardRepairRecoveryAuthority(original, current state.GitSnapshot) bool {
	if original.Head != current.Head || original.IndexDigest != current.IndexDigest {
		return false
	}
	if original.ParentFiles == nil || current.ParentFiles == nil {
		return false
	}
	return state.SameParentFileStates(*original.ParentFiles, *current.ParentFiles)
}

func filterGuardRepairIntegrationDirty(files []state.StopDirtyFile, excluded map[string]struct{}) []state.StopDirtyFile {
	result := make([]state.StopDirtyFile, 0, len(files))
	for _, file := range files {
		if _, ok := excluded[file.Path]; ok {
			continue
		}
		result = append(result, file)
	}
	return result
}
