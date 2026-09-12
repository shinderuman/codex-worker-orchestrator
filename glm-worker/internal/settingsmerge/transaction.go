package settingsmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type mergeTransactionJournal struct {
	Version int                    `json:"version"`
	Files   []mergeTransactionFile `json:"files"`
}

type mergeTransactionFile struct {
	Path       string      `json:"path"`
	PreExisted bool        `json:"pre_existed"`
	PreData    []byte      `json:"pre_data,omitempty"`
	PreMode    os.FileMode `json:"pre_mode,omitempty"`
	PostData   []byte      `json:"post_data"`
	PostMode   os.FileMode `json:"post_mode"`
}

const mergeTransactionVersion = 1
const mergeTransactionSuffix = ".merge-transaction.json"

func mergeTransactionPath(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), managedStateDir, filepath.Base(targetPath)+mergeTransactionSuffix)
}

func recoverSettingsTransaction(targetPath string, writeFn writeFileFunc) error {
	journalPath := mergeTransactionPath(targetPath)
	journal, err := loadMergeTransactionJournal(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settings transaction journal: %w", err)
	}
	if err := validateMergeTransactionJournal(targetPath, journal); err != nil {
		return err
	}
	if err := validateMergeTransactionCurrentFiles(journal); err != nil {
		return err
	}
	if err := restoreMergeTransaction(journal, writeFn); err != nil {
		return fmt.Errorf("recover settings transaction: %w", err)
	}
	return removeMergeTransactionJournal(journalPath, "remove recovered settings transaction journal")
}

func commitRecoverableTransaction(targetPath string, plans []plannedWrite, writeFn writeFileFunc) error {
	journal, err := newMergeTransactionJournal(plans)
	if err != nil {
		return err
	}
	journalPath := mergeTransactionPath(targetPath)
	if err := saveMergeTransactionJournal(journalPath, journal); err != nil {
		return fmt.Errorf("persist settings transaction journal: %w", err)
	}
	if err := applyMergeTransactionPlans(plans, writeFn); err != nil {
		return rollbackFailedMergeTransaction(journalPath, journal, writeFn, err)
	}
	return removeMergeTransactionJournal(journalPath, "finalize settings transaction journal")
}

func applyMergeTransactionPlans(plans []plannedWrite, writeFn writeFileFunc) error {
	for _, plan := range plans {
		if err := writeFn(plan.path, plan.data, plan.mode); err != nil {
			return err
		}
	}
	return nil
}

func rollbackFailedMergeTransaction(journalPath string, journal mergeTransactionJournal, writeFn writeFileFunc, cause error) error {
	if err := restoreMergeTransaction(journal, writeFn); err != nil {
		return fmt.Errorf("%w (rollback failed: %w)", cause, err)
	}
	if err := removeMergeTransactionJournal(journalPath, "remove settings transaction journal after rollback"); err != nil {
		return fmt.Errorf("%w (rollback cleanup failed: %w)", cause, err)
	}
	return cause
}

func removeMergeTransactionJournal(path, operation string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func newMergeTransactionJournal(plans []plannedWrite) (mergeTransactionJournal, error) {
	journal := mergeTransactionJournal{Version: mergeTransactionVersion, Files: make([]mergeTransactionFile, 0, len(plans))}
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if seen[plan.path] {
			return mergeTransactionJournal{}, fmt.Errorf("duplicate settings transaction path %s", plan.path)
		}
		seen[plan.path] = true
		pre, err := captureMergeTransactionFile(plan.path)
		if err != nil {
			return mergeTransactionJournal{}, err
		}
		journal.Files = append(journal.Files, mergeTransactionFile{
			Path:       plan.path,
			PreExisted: pre.existed,
			PreData:    pre.data,
			PreMode:    pre.mode,
			PostData:   append([]byte(nil), plan.data...),
			PostMode:   normalizedFileMode(plan.mode),
		})
	}
	return journal, nil
}

func captureMergeTransactionFile(path string) (fileRestore, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileRestore{}, nil
	}
	if err != nil {
		return fileRestore{}, fmt.Errorf("capture settings transaction preimage %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileRestore{}, fmt.Errorf("stat settings transaction preimage %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fileRestore{}, fmt.Errorf("settings transaction path is not a regular file: %s", path)
	}
	return fileRestore{existed: true, data: data, mode: info.Mode().Perm()}, nil
}

func saveMergeTransactionJournal(path string, journal mergeTransactionJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func loadMergeTransactionJournal(path string) (mergeTransactionJournal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mergeTransactionJournal{}, err
	}
	var journal mergeTransactionJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return mergeTransactionJournal{}, err
	}
	if journal.Version != mergeTransactionVersion {
		return mergeTransactionJournal{}, fmt.Errorf("unsupported settings transaction journal version %d", journal.Version)
	}
	return journal, nil
}

func validateMergeTransactionJournal(targetPath string, journal mergeTransactionJournal) error {
	allowed := map[string]bool{
		targetPath:                   true,
		statePathFor(targetPath):     true,
		ManagedStatePath(targetPath): true,
	}
	if len(journal.Files) == 0 || len(journal.Files) > len(allowed) {
		return fmt.Errorf("settings transaction journal file set is invalid")
	}
	seen := make(map[string]bool, len(journal.Files))
	for _, file := range journal.Files {
		if !allowed[file.Path] || seen[file.Path] {
			return fmt.Errorf("settings transaction journal contains unexpected path %s", file.Path)
		}
		seen[file.Path] = true
		if !file.PreExisted && (len(file.PreData) != 0 || file.PreMode != 0) {
			return fmt.Errorf("settings transaction journal has invalid absent preimage for %s", file.Path)
		}
	}
	return nil
}

func validateMergeTransactionCurrentFiles(journal mergeTransactionJournal) error {
	for _, file := range journal.Files {
		current, err := captureMergeTransactionFile(file.Path)
		if err != nil {
			return err
		}
		if mergeTransactionMatchesPre(current, file) || mergeTransactionMatchesPost(current, file) {
			continue
		}
		return fmt.Errorf("settings transaction path changed after interrupted merge: %s", file.Path)
	}
	return nil
}

func mergeTransactionMatchesPre(current fileRestore, file mergeTransactionFile) bool {
	if current.existed != file.PreExisted {
		return false
	}
	if !current.existed {
		return true
	}
	return current.mode.Perm() == file.PreMode.Perm() && bytes.Equal(current.data, file.PreData)
}

func mergeTransactionMatchesPost(current fileRestore, file mergeTransactionFile) bool {
	return current.existed && current.mode.Perm() == file.PostMode.Perm() && bytes.Equal(current.data, file.PostData)
}

func restoreMergeTransaction(journal mergeTransactionJournal, writeFn writeFileFunc) error {
	var errs []error
	for i := len(journal.Files) - 1; i >= 0; i-- {
		file := journal.Files[i]
		if !file.PreExisted {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", file.Path, err))
			}
			continue
		}
		if err := writeFn(file.Path, file.PreData, file.PreMode); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", file.Path, err))
		}
	}
	return errors.Join(errs...)
}

func normalizedFileMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o600
	}
	return mode.Perm()
}
