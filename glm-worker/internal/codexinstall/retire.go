package codexinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

func Retire(codexDir string, output io.Writer) error {
	codexDir = filepath.Clean(codexDir)
	if codexDir == "." {
		return fmt.Errorf("codex directory must be explicit")
	}
	lock, err := acquireInstallLock(codexDir)
	if err != nil {
		return err
	}
	if err := recoverInterruptedInstall(codexDir); err != nil {
		return joinInstallLockError(err, lock.Close())
	}
	err = retireLocked(codexDir, output)
	return joinInstallLockError(err, lock.Close())
}

func retireLocked(codexDir string, output io.Writer) error {
	state, exists, stateBackup, err := loadInstallStateSnapshot(codexDir)
	if err != nil {
		return err
	}
	if !exists {
		_, _ = fmt.Fprintln(output, "Codex: unchanged (no installer ownership state)")
		return nil
	}
	paths, post, err := planCodexRetire(codexDir, state)
	if err != nil {
		return err
	}
	backups, err := snapshotCodexRetirePaths(paths)
	if err != nil {
		return err
	}
	if err := verifyCodexRetireInputs(codexDir, state, stateBackup, backups); err != nil {
		return err
	}
	destination, err := filepath.Abs(codexDir)
	if err != nil {
		return err
	}
	journal, err := buildInstallTransactionJournal(codexDir, filepath.Clean(destination), backups, post)
	if err != nil {
		return err
	}
	if err := saveInstallTransactionJournal(installTransactionPath(codexDir), journal); err != nil {
		return err
	}
	if err := applyCodexRetire(codexDir, backups, post, stateBackup); err != nil {
		return rollbackCodexRetire(codexDir, err)
	}
	if err := removeInstallTransactionJournal(codexDir, "finish Codex retirement"); err != nil {
		return err
	}
	if err := cleanEmptyCodexRetireDirs(codexDir, state.Files); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Codex: retired %d owned instruction/rule/prompt records and managed config assignment\n", len(state.Files))
	return nil
}

func planCodexRetire(codexDir string, state installState) ([]string, map[string]installTransactionImage, error) {
	paths := []string{statePath(codexDir)}
	post := map[string]installTransactionImage{filepath.Clean(statePath(codexDir)): {}}
	for _, record := range state.Files {
		if err := validateManagedPathAncestors(codexDir, record.Path); err != nil {
			return nil, nil, err
		}
		path := filepath.Join(codexDir, filepath.FromSlash(record.Path))
		backup, err := captureInstallBackup(path)
		if err != nil {
			return nil, nil, err
		}
		if backup.Exists && digestBytes(backup.Content) != record.SHA256 {
			return nil, nil, fmt.Errorf("refusing modified Codex managed file %s", record.Path)
		}
		paths = append(paths, path)
		post[filepath.Clean(path)] = installTransactionImage{}
	}
	if record, owned := state.Config[managedConfigKey]; owned {
		path, image, changed, err := planCodexConfigRetire(codexDir, record)
		if err != nil {
			return nil, nil, err
		}
		if changed {
			paths = append(paths, path)
			post[filepath.Clean(path)] = image
		}
	}
	sort.Strings(paths)
	return paths, post, nil
}

func planCodexConfigRetire(codexDir string, record managedConfigRecord) (string, installTransactionImage, bool, error) {
	path := filepath.Join(codexDir, "config.toml")
	backup, err := captureInstallBackup(path)
	if err != nil {
		return "", installTransactionImage{}, false, err
	}
	if !backup.Exists {
		return path, installTransactionImage{}, false, nil
	}
	assignment, found, err := findTopLevelAssignment(backup.Content, managedConfigKey)
	if err != nil {
		return "", installTransactionImage{}, false, err
	}
	if !found {
		return path, installTransactionImage{}, false, nil
	}
	if digestBytes([]byte(assignment.Line)) != record.LineSHA256 {
		return "", installTransactionImage{}, false, fmt.Errorf("refusing modified Codex managed config key: %s", managedConfigKey)
	}
	next := replaceAssignmentLine(backup.Content, assignment.Index, "")
	if bytes.Equal(next, backup.Content) {
		return path, installTransactionImage{}, false, nil
	}
	return path, installTransactionImage{Exists: true, Content: next, Mode: backup.Mode.Perm()}, true, nil
}

func snapshotCodexRetirePaths(paths []string) ([]installBackup, error) {
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

func verifyCodexRetireInputs(codexDir string, state installState, stateBackup installBackup, backups []installBackup) error {
	if !bytes.Equal(backupsAt(backups, statePath(codexDir)).Content, stateBackup.Content) {
		return fmt.Errorf("codex ownership state changed during retire preparation")
	}
	for _, record := range state.Files {
		file := backupsAt(backups, filepath.Join(codexDir, filepath.FromSlash(record.Path)))
		if file.Exists && digestBytes(file.Content) != record.SHA256 {
			return fmt.Errorf("codex managed file changed during retire preparation: %s", record.Path)
		}
	}
	if record, owned := state.Config[managedConfigKey]; owned {
		return verifyCodexRetireConfig(codexDir, record, backups)
	}
	return nil
}

func verifyCodexRetireConfig(codexDir string, record managedConfigRecord, backups []installBackup) error {
	config := backupsAt(backups, filepath.Join(codexDir, "config.toml"))
	if config.Path == "" || !config.Exists {
		return nil
	}
	assignment, found, err := findTopLevelAssignment(config.Content, managedConfigKey)
	if err != nil {
		return err
	}
	if found && digestBytes([]byte(assignment.Line)) != record.LineSHA256 {
		return fmt.Errorf("codex managed config changed during retire preparation")
	}
	return nil
}

func applyCodexRetire(codexDir string, backups []installBackup, post map[string]installTransactionImage, stateBackup installBackup) error {
	for _, backup := range backups {
		if backup.Path == statePath(codexDir) {
			continue
		}
		if err := applyCodexRetirePath(backup, post[filepath.Clean(backup.Path)]); err != nil {
			return err
		}
	}
	if err := requireCodexRetireSnapshot(stateBackup); err != nil {
		return err
	}
	return os.Remove(statePath(codexDir))
}

func applyCodexRetirePath(backup installBackup, image installTransactionImage) error {
	if err := requireCodexRetireSnapshot(backup); err != nil {
		return err
	}
	if image.Exists {
		return writeAtomic(backup.Path, image.Content, image.Mode)
	}
	if !backup.Exists {
		return nil
	}
	return os.Remove(backup.Path)
}

func requireCodexRetireSnapshot(backup installBackup) error {
	matches, err := installBackupMatchesCurrent(backup)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("codex retire surface changed: %s", backup.Path)
	}
	return nil
}

func rollbackCodexRetire(codexDir string, cause error) error {
	if recoveryErr := recoverInterruptedInstall(codexDir); recoveryErr != nil {
		return errors.Join(cause, fmt.Errorf("codex retire recovery is required: %w", recoveryErr))
	}
	return cause
}

func cleanEmptyCodexRetireDirs(codexDir string, files []managedFileRecord) error {
	dirs := map[string]bool{filepath.Dir(statePath(codexDir)): true}
	for _, record := range files {
		dir := filepath.Dir(filepath.Join(codexDir, filepath.FromSlash(record.Path)))
		for dir != codexDir && dir != "." && dir != string(filepath.Separator) {
			dirs[dir] = true
			dir = filepath.Dir(dir)
		}
	}
	sorted := make([]string, 0, len(dirs))
	for dir := range dirs {
		sorted = append(sorted, dir)
	}
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, dir := range sorted {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) &&
			!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
			return err
		}
	}
	return nil
}

func backupsAt(backups []installBackup, path string) installBackup {
	for _, backup := range backups {
		if backup.Path == path {
			return backup
		}
	}
	return installBackup{}
}
