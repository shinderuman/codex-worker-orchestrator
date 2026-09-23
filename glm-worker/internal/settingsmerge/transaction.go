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

func commitRecoverableTransaction(targetPath string, plans []plannedWrite, inputs map[string]fileRestore, writeFn writeFileFunc) error {
	if err := validateMergeInputSnapshots(inputs); err != nil {
		return err
	}
	journal, err := newMergeTransactionJournal(plans, inputs)
	if err != nil {
		return err
	}
	journalPath := mergeTransactionPath(targetPath)
	if err := saveMergeTransactionJournal(journalPath, journal); err != nil {
		return fmt.Errorf("persist settings transaction journal: %w", err)
	}
	current := cloneMergeInputSnapshots(inputs)
	if err := applyMergeTransactionPlans(plans, current, writeFn); err != nil {
		return rollbackFailedMergeTransaction(journalPath, journal, writeFn, err)
	}
	if err := validateMergeInputSnapshots(current); err != nil {
		return rollbackFailedMergeTransaction(journalPath, journal, writeFn, err)
	}
	return removeMergeTransactionJournal(journalPath, "finalize settings transaction journal")
}

func applyMergeTransactionPlans(plans []plannedWrite, current map[string]fileRestore, writeFn writeFileFunc) error {
	for _, plan := range plans {
		if err := validateMergeInputSnapshots(current); err != nil {
			return err
		}
		if err := writeFn(plan.path, plan.data, plan.mode); err != nil {
			return err
		}
		current[plan.path] = fileRestore{
			existed: true,
			data:    append([]byte(nil), plan.data...),
			mode:    normalizedFileMode(plan.mode),
		}
	}
	return nil
}

func validateMergeInputSnapshots(inputs map[string]fileRestore) error {
	for path, expected := range inputs {
		current, err := captureMergeTransactionFile(path)
		if err != nil {
			return err
		}
		if !fileRestoreMatches(current, expected) {
			return fmt.Errorf("settings merge input changed after planning: %s", path)
		}
	}
	return nil
}

func cloneMergeInputSnapshots(inputs map[string]fileRestore) map[string]fileRestore {
	clone := make(map[string]fileRestore, len(inputs))
	for path, snapshot := range inputs {
		clone[path] = fileRestore{
			existed: snapshot.existed,
			data:    append([]byte(nil), snapshot.data...),
			mode:    snapshot.mode,
		}
	}
	return clone
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

func newMergeTransactionJournal(plans []plannedWrite, provided ...map[string]fileRestore) (mergeTransactionJournal, error) {
	if len(provided) > 1 {
		return mergeTransactionJournal{}, fmt.Errorf("multiple settings transaction preimage sets")
	}
	var inputs map[string]fileRestore
	if len(provided) == 1 {
		inputs = provided[0]
	} else {
		inputs = make(map[string]fileRestore, len(plans))
		for _, plan := range plans {
			pre, err := captureMergeTransactionFile(plan.path)
			if err != nil {
				return mergeTransactionJournal{}, err
			}
			inputs[plan.path] = pre
		}
	}
	return buildMergeTransactionJournal(plans, inputs)
}

func buildMergeTransactionJournal(plans []plannedWrite, inputs map[string]fileRestore) (mergeTransactionJournal, error) {
	journal := mergeTransactionJournal{Version: mergeTransactionVersion, Files: make([]mergeTransactionFile, 0, len(plans))}
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if seen[plan.path] {
			return mergeTransactionJournal{}, fmt.Errorf("duplicate settings transaction path %s", plan.path)
		}
		seen[plan.path] = true
		pre, ok := inputs[plan.path]
		if !ok {
			return mergeTransactionJournal{}, fmt.Errorf("settings transaction preimage is missing for %s", plan.path)
		}
		journal.Files = append(journal.Files, mergeTransactionFile{
			Path:       plan.path,
			PreExisted: pre.existed,
			PreData:    append([]byte(nil), pre.data...),
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

func fileRestoreMatches(left, right fileRestore) bool {
	if left.existed != right.existed {
		return false
	}
	if !left.existed {
		return true
	}
	return left.mode.Perm() == right.mode.Perm() && bytes.Equal(left.data, right.data)
}

func restoreMergeTransaction(journal mergeTransactionJournal, writeFn writeFileFunc) error {
	var errs []error
	for i := len(journal.Files) - 1; i >= 0; i-- {
		if err := restoreMergeTransactionFile(journal.Files[i], writeFn); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreMergeTransactionFile(file mergeTransactionFile, writeFn writeFileFunc) error {
	current, err := captureMergeTransactionFile(file.Path)
	if err != nil {
		return err
	}
	if mergeTransactionMatchesPre(current, file) {
		return nil
	}
	if !mergeTransactionMatchesPost(current, file) {
		return fmt.Errorf("settings transaction rollback refused concurrent edit: %s", file.Path)
	}
	if !file.PreExisted {
		if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", file.Path, err)
		}
		return nil
	}
	if err := writeFn(file.Path, file.PreData, file.PreMode); err != nil {
		return fmt.Errorf("restore %s: %w", file.Path, err)
	}
	return nil
}

func normalizedFileMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o600
	}
	return mode.Perm()
}
