package codexinstall

import (
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

func applyInstallWithStateWriter(preparation installPreparation, stdout io.Writer, writeStateFn installStateWriter) error {
	backups, err := captureInstallBackups(preparation)
	if err != nil {
		return err
	}
	output := func(format string, args ...any) {
		_, _ = fmt.Fprintf(stdout, format, args...)
	}
	files, err := applyFileInstallPlan(preparation.codexDir, preparation.filePlan, output)
	if err != nil {
		return rollbackInstall(backups, err)
	}
	if err := applyConfigInstallPlan(preparation.configPlan, output); err != nil {
		return rollbackInstall(backups, err)
	}
	next := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
	if preparation.configPlan.Record != nil {
		next.Config[managedConfigKey] = *preparation.configPlan.Record
	}
	if err := writeStateFn(preparation.codexDir, next); err != nil {
		return rollbackInstall(backups, err)
	}
	if preparation.stateExists {
		return nil
	}
	return removeLegacyManifest(preparation.codexDir, preparation.legacy.Present)
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

func rollbackInstall(backups []installBackup, cause error) error {
	errs := []error{cause}
	for index := len(backups) - 1; index >= 0; index-- {
		if err := restoreInstallBackup(backups[index]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
