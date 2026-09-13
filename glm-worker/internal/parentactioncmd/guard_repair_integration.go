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
) (state.GuardRepairRecord, error) {
	if record.Status != state.GuardRepairRunning || record.Integration != nil {
		return record, fmt.Errorf("guard repair integration requires running transaction")
	}
	preimages, err := captureGuardRepairFiles(repoRoot, changed)
	if err != nil {
		return record, err
	}
	postimages, err := captureGuardRepairFiles(repairRoot, changed)
	if err != nil {
		return record, err
	}
	files, err := guardRepairIntegrationFiles(preimages, postimages)
	if err != nil {
		return record, err
	}
	record.Status = state.GuardRepairIntegrating
	record.Integration = &state.GuardRepairIntegration{
		RepositoryBoundary: origin.boundary,
		StopDirtyFiles:      append([]state.StopDirtyFile(nil), origin.checkpoint.StopDirtyFiles...),
		Files:               files,
	}
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func guardRepairIntegrationFiles(preimages, postimages []guardRepairFileBackup) ([]state.GuardRepairIntegrationFile, error) {
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
	if record.Status != state.GuardRepairIntegrating || record.Integration == nil {
		return fmt.Errorf("guard repair transaction is not integrating")
	}
	record.Status = state.GuardRepairReady
	record.Integration = nil
	return st.SaveGuardRepairRecord(record)
}

func recoverGuardRepairIntegrationIfNeeded(cfg config.AppConfig, st *state.StateStore) error {
	record, err := st.LoadGuardRepairRecord()
	if errors.Is(err, state.ErrNoGuardRepairRecord) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Status != state.GuardRepairIntegrating {
		return nil
	}
	lock, err := repolock.AcquireWait(st.LockPath())
	if err != nil {
		return err
	}
	record, err = st.LoadGuardRepairRecord()
	if err != nil {
		return errors.Join(err, lock.Close())
	}
	if record.Status != state.GuardRepairIntegrating {
		return lock.Close()
	}
	return errors.Join(rollbackGuardRepairIntegration(cfg, st, record), lock.Close())
}

func rollbackGuardRepairIntegration(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) error {
	checkpoint, err := validateGuardRepairIntegrationRecovery(cfg, st, record)
	if err != nil {
		return err
	}
	integration := record.Integration
	backups := make([]guardRepairFileBackup, 0, len(integration.Files))
	for _, file := range integration.Files {
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
	checkpoint.StopDirtyFiles = append([]state.StopDirtyFile(nil), integration.StopDirtyFiles...)
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		return fmt.Errorf("restore interrupted guard repair checkpoint: %w", err)
	}
	restored, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("verify interrupted guard repair rollback: %w", err)
	}
	if !sameRepositoryBoundary(integration.RepositoryBoundary, restored) {
		return fmt.Errorf("interrupted guard repair rollback did not restore original repository boundary")
	}
	record.Status = state.GuardRepairRequested
	record.RepairedDigest = ""
	record.Integration = nil
	record.ClearResumeProof()
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return fmt.Errorf("reset guard repair record after interrupted integration: %w", err)
	}
	return nil
}

func validateGuardRepairIntegrationRecovery(
	cfg config.AppConfig,
	st *state.StateStore,
	record state.GuardRepairRecord,
) (state.ResumeCheckpoint, error) {
	if record.Status != state.GuardRepairIntegrating || record.Integration == nil {
		return state.ResumeCheckpoint{}, fmt.Errorf("guard repair transaction has no integration recovery state")
	}
	if st.ReadOr("task.id", "") != record.TaskID || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return state.ResumeCheckpoint{}, fmt.Errorf("guard repair integration belongs to a stale or foreign task")
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return state.ResumeCheckpoint{}, fmt.Errorf("verify guard repair integration checkpoint: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.Phase != record.Phase {
		return state.ResumeCheckpoint{}, fmt.Errorf("guard repair integration checkpoint provenance does not match current checkpoint")
	}
	integration := record.Integration
	if err := validateGuardRepairIntegrationPaths(integration); err != nil {
		return state.ResumeCheckpoint{}, err
	}
	if err := validateGuardRepairIntegrationAuthority(cfg, integration); err != nil {
		return state.ResumeCheckpoint{}, err
	}
	if err := validateGuardRepairIntegrationFiles(cfg.RepoRoot, integration); err != nil {
		return state.ResumeCheckpoint{}, err
	}
	if err := validateGuardRepairIntegrationDirty(cfg, integration); err != nil {
		return state.ResumeCheckpoint{}, err
	}
	return checkpoint, nil
}

func validateGuardRepairIntegrationPaths(integration *state.GuardRepairIntegration) error {
	for _, file := range integration.Files {
		if !guardrepair.IsAllowed(file.Path) {
			return fmt.Errorf("guard repair integration contains out-of-scope path %s", file.Path)
		}
	}
	return nil
}

func validateGuardRepairIntegrationAuthority(cfg config.AppConfig, integration *state.GuardRepairIntegration) error {
	current, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if !sameGuardRepairRecoveryAuthority(integration.RepositoryBoundary, current) {
		return fmt.Errorf("repository authority changed after guard repair integration began")
	}
	return nil
}

func validateGuardRepairIntegrationFiles(repoRoot string, integration *state.GuardRepairIntegration) error {
	paths := make([]string, 0, len(integration.Files))
	for _, file := range integration.Files {
		paths = append(paths, file.Path)
	}
	current, err := captureGuardRepairFiles(repoRoot, paths)
	if err != nil {
		return fmt.Errorf("verify interrupted guard repair files: %w", err)
	}
	for i, file := range integration.Files {
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

func validateGuardRepairIntegrationDirty(cfg config.AppConfig, integration *state.GuardRepairIntegration) error {
	currentDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return err
	}
	paths := make(map[string]struct{}, len(integration.Files))
	for _, file := range integration.Files {
		paths[file.Path] = struct{}{}
	}
	if diff := state.DescribeStopDirtyDiff(
		filterGuardRepairIntegrationDirty(integration.StopDirtyFiles, paths),
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
