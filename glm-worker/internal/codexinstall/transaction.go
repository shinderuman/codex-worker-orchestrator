package codexinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type installBackup struct {
	Path    string
	Exists  bool
	Content []byte
	Mode    os.FileMode
}

type installStateWriter func(string, installState) error

type installMutationTracker struct {
	backups map[string]installBackup
	written map[string]installBackup
	order   []string
}

func applyInstallWithStateWriter(preparation installPreparation, stdout io.Writer, writeStateFn installStateWriter) error {
	if err := requireInstallStateUnchanged(preparation); err != nil {
		return err
	}
	backups, err := captureInstallBackups(preparation)
	if err != nil {
		return err
	}
	next := plannedInstallState(preparation)
	journal, err := newInstallTransactionJournal(preparation, backups, next)
	if err != nil {
		return err
	}
	if err := saveInstallTransactionJournal(installTransactionPath(preparation.codexDir), journal); err != nil {
		return err
	}

	tracker := newInstallMutationTracker(backups)
	rollback := func(cause error) error {
		return rollbackInstallAttempt(preparation.codexDir, tracker, cause)
	}
	var pendingOutput bytes.Buffer
	output := func(format string, args ...any) { _, _ = fmt.Fprintf(&pendingOutput, format, args...) }
	files, err := applyFileInstallPlan(preparation.codexDir, preparation.filePlan, preparation.state, preparation.stateExists, tracker.record, output)
	if err != nil {
		return rollback(err)
	}
	if err := applyConfigInstallPlan(preparation.configPlan, tracker.record, output); err != nil {
		return rollback(err)
	}
	if err := validateManagedFilesForStateCommit(preparation.codexDir, files); err != nil {
		return rollback(err)
	}
	if err := validateConfigForStateCommit(preparation.configPlan); err != nil {
		return rollback(err)
	}
	if err := requireInstallStateUnchanged(preparation); err != nil {
		return rollback(err)
	}
	if err := writeStateFn(preparation.codexDir, next); err != nil {
		return rollback(err)
	}
	if err := removeInstallTransactionJournal(preparation.codexDir, "finalize Codex install transaction"); err != nil {
		return err
	}
	_, _ = io.Copy(stdout, &pendingOutput)
	return nil
}

func rollbackInstallAttempt(codexDir string, tracker *installMutationTracker, cause error) error {
	if rollbackErr := tracker.rollbackMutations(); rollbackErr != nil {
		return errors.Join(cause, rollbackErr)
	}
	matches, err := installTransactionPreimagesMatch(codexDir)
	if err != nil {
		return errors.Join(cause, err)
	}
	if !matches {
		return errors.Join(cause, fmt.Errorf("install rollback incomplete; durable recovery required"))
	}
	if err := removeInstallTransactionJournal(codexDir, "remove Codex install transaction journal after rollback"); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func requireInstallStateUnchanged(preparation installPreparation) error {
	same, err := installBackupMatchesCurrent(preparation.stateBackup)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("codex install state changed after preparation; refusing to continue")
	}
	return nil
}

func loadInstallStateSnapshot(codexDir string) (installState, bool, installBackup, error) {
	if err := validateManagedPathAncestors(codexDir, stateRelativePath); err != nil {
		return installState{}, false, installBackup{}, err
	}
	backup, err := captureInstallBackup(statePath(codexDir))
	if err != nil {
		return installState{}, false, installBackup{}, err
	}
	if !backup.Exists {
		return installState{Version: stateVersion, Config: map[string]managedConfigRecord{}}, false, backup, nil
	}
	state, err := decodeInstallState(backup.Content)
	if err != nil {
		return installState{}, false, installBackup{}, err
	}
	return state, true, backup, nil
}

func captureInstallBackups(preparation installPreparation) ([]installBackup, error) {
	paths := installMutationPaths(preparation)
	backups := make([]installBackup, 0, len(paths))
	for _, path := range paths {
		backup, err := captureInstallBackup(path)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, nil
}

func installMutationPaths(preparation installPreparation) []string {
	unique := map[string]bool{statePath(preparation.codexDir): true}
	for _, file := range preparation.filePlan.Desired {
		unique[filepath.Join(preparation.codexDir, filepath.FromSlash(file.Path))] = true
	}
	for _, path := range preparation.filePlan.Remove {
		unique[filepath.Join(preparation.codexDir, filepath.FromSlash(path))] = true
	}
	if preparation.configPlan.Changed {
		unique[preparation.configPlan.Path] = true
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func captureInstallBackup(path string) (installBackup, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return installBackup{Path: path}, nil
	}
	if err != nil {
		return installBackup{}, fmt.Errorf("stat install rollback surface %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return installBackup{}, fmt.Errorf("install rollback surface is not a regular file: %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return installBackup{}, fmt.Errorf("read install rollback surface %s: %w", path, err)
	}
	return installBackup{Path: path, Exists: true, Content: content, Mode: info.Mode().Perm()}, nil
}

func restoreInstallBackup(backup installBackup) error {
	if backup.Exists {
		if err := writeAtomic(backup.Path, backup.Content, backup.Mode); err != nil {
			return fmt.Errorf("restore install rollback surface %s: %w", backup.Path, err)
		}
		return nil
	}
	if err := os.Remove(backup.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove newly-created install surface %s during rollback: %w", backup.Path, err)
	}
	return nil
}

func newInstallMutationTracker(backups []installBackup) *installMutationTracker {
	m := make(map[string]installBackup, len(backups))
	for _, b := range backups {
		m[b.Path] = b
	}
	return &installMutationTracker{backups: m, written: map[string]installBackup{}}
}

func (t *installMutationTracker) record(path string) error {
	snapshot, err := captureInstallBackup(path)
	if err != nil {
		return fmt.Errorf("capture installed surface after mutation %s: %w", path, err)
	}
	if _, seen := t.written[path]; !seen {
		t.order = append(t.order, path)
	}
	t.written[path] = snapshot
	return nil
}

func (t *installMutationTracker) rollbackMutations() error {
	var errs []error
	for i := len(t.order) - 1; i >= 0; i-- {
		path := t.order[i]
		written := t.written[path]
		same, err := installBackupMatchesCurrent(written)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !same {
			errs = append(errs, fmt.Errorf("install rollback skipped concurrently modified surface: %s", path))
			continue
		}
		backup, ok := t.backups[path]
		if !ok {
			errs = append(errs, fmt.Errorf("install rollback backup missing for surface: %s", path))
			continue
		}
		if err := restoreInstallBackup(backup); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func installBackupMatchesCurrent(expected installBackup) (bool, error) {
	current, err := captureInstallBackup(expected.Path)
	if err != nil {
		return false, err
	}
	return current.Exists == expected.Exists && current.Mode == expected.Mode && bytes.Equal(current.Content, expected.Content), nil
}
