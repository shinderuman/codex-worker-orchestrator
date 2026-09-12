package parentactioncmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func beginGuardRepairIntegration(
	st *state.StateStore,
	record state.GuardRepairRecord,
	origin guardRepairOrigin,
	repoRoot string,
	changed []string,
) (state.GuardRepairIntegrationJournal, error) {
	backups, err := captureGuardRepairFiles(repoRoot, changed)
	if err != nil {
		return state.GuardRepairIntegrationJournal{}, err
	}
	files := make([]state.GuardRepairIntegrationFile, 0, len(backups))
	for _, backup := range backups {
		files = append(files, state.GuardRepairIntegrationFile{
			Path:    backup.path,
			Content: append([]byte(nil), backup.content...),
			Mode:    uint32(backup.mode.Perm()),
			Exists:  backup.exists,
		})
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

func recoverGuardRepairIntegrationIfNeeded(cfg config.AppConfig, st *state.StateStore) error {
	journal, err := st.LoadGuardRepairIntegrationJournal()
	if errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		return nil
	}
	if err != nil {
		return err
	}
	return rollbackGuardRepairIntegration(cfg, st, journal)
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
	if st.ReadOr("task.id", "") != journal.TaskID || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal belongs to a stale or foreign task")
	}
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
	default:
		return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal cannot recover from record status %q", record.Status)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return state.GuardRepairRecord{}, fmt.Errorf("verify guard repair integration checkpoint: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.Phase != journal.Phase {
		return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal checkpoint provenance does not match current checkpoint")
	}
	for _, file := range journal.Files {
		if !guardrepair.IsAllowed(file.Path) {
			return state.GuardRepairRecord{}, fmt.Errorf("guard repair integration journal contains out-of-scope path %s", file.Path)
		}
	}
	current, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return state.GuardRepairRecord{}, err
	}
	if !sameGuardRepairRecoveryAuthority(journal.RepositoryBoundary, current) {
		return state.GuardRepairRecord{}, fmt.Errorf("repository authority changed after guard repair integration journal was created")
	}
	currentDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return state.GuardRepairRecord{}, err
	}
	paths := make(map[string]struct{}, len(journal.Files))
	for _, file := range journal.Files {
		paths[file.Path] = struct{}{}
	}
	if diff := state.DescribeStopDirtyDiff(
		filterGuardRepairIntegrationDirty(journal.ResumeCheckpoint.StopDirtyFiles, paths),
		filterGuardRepairIntegrationDirty(currentDirty, paths),
	); diff != "" {
		return state.GuardRepairRecord{}, fmt.Errorf("repository source outside interrupted guard repair changed: %s", diff)
	}
	return record, nil
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
