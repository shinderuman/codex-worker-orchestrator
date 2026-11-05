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
    if codexDir == "." { return fmt.Errorf("Codex directory must be explicit") }
    lock, err := acquireInstallLock(codexDir)
    if err != nil { return err }
    if err := recoverInterruptedInstall(codexDir); err != nil {
        return joinInstallLockError(err, lock.Close())
    }
    err = retireLocked(codexDir, output)
    return joinInstallLockError(err, lock.Close())
}

func retireLocked(codexDir string, output io.Writer) error {
    state, exists, stateBackup, err := loadInstallStateSnapshot(codexDir)
    if err != nil { return err }
    if !exists {
        _, _ = fmt.Fprintln(output, "Codex: unchanged (no installer ownership state)")
        return nil
    }
    paths := []string{statePath(codexDir)}
    post := map[string]installTransactionImage{filepath.Clean(statePath(codexDir)): {}}
    for _, record := range state.Files {
        if err := validateManagedPathAncestors(codexDir, record.Path); err != nil { return err }
        target := filepath.Join(codexDir, filepath.FromSlash(record.Path))
        backup, err := captureInstallBackup(target)
        if err != nil { return err }
        if backup.Exists && digestBytes(backup.Content) != record.SHA256 {
            return fmt.Errorf("refusing modified Codex managed file %s", record.Path)
        }
        paths = append(paths, target)
        post[filepath.Clean(target)] = installTransactionImage{}
    }
    if record, ok := state.Config[managedConfigKey]; ok {
        configPath := filepath.Join(codexDir, "config.toml")
        backup, err := captureInstallBackup(configPath)
        if err != nil { return err }
        if backup.Exists {
            assignment, found, err := findTopLevelAssignment(backup.Content, managedConfigKey)
            if err != nil { return err }
            if found {
                if digestBytes([]byte(assignment.Line)) != record.LineSHA256 {
                    return fmt.Errorf("refusing modified Codex managed config key: %s", managedConfigKey)
                }
                next := replaceAssignmentLine(backup.Content, assignment.Index, "")
                if !bytes.Equal(next, backup.Content) {
                    paths = append(paths, configPath)
                    post[filepath.Clean(configPath)] = installTransactionImage{Exists: true, Content: next, Mode: backup.Mode.Perm()}
                }
            }
        }
    }
    sort.Strings(paths)
    backups := make([]installBackup, 0, len(paths))
    for _, path := range paths {
        backup, err := captureInstallBackup(path)
        if err != nil { return err }
        backups = append(backups, backup)
    }
    if !bytes.Equal(backupsAt(backups, statePath(codexDir)).Content, stateBackup.Content) {
        return fmt.Errorf("Codex ownership state changed during retire preparation")
    }
    destination, err := filepath.Abs(codexDir)
    if err != nil { return err }
    journal, err := buildInstallTransactionJournal(codexDir, filepath.Clean(destination), backups, post)
    if err != nil { return err }
    if err := saveInstallTransactionJournal(installTransactionPath(codexDir), journal); err != nil { return err }

    fail := func(cause error) error {
        if recoveryErr := recoverInterruptedInstall(codexDir); recoveryErr != nil {
            return errors.Join(cause, fmt.Errorf("Codex retire recovery is required: %w", recoveryErr))
        }
        return cause
    }
    for _, backup := range backups {
        if backup.Path == statePath(codexDir) { continue }
        if matches, err := installBackupMatchesCurrent(backup); err != nil || !matches {
            return fail(fmt.Errorf("Codex retire surface changed: %s: %v", backup.Path, err))
        }
        image := post[filepath.Clean(backup.Path)]
        if image.Exists {
            if err := writeAtomic(backup.Path, image.Content, image.Mode); err != nil { return fail(err) }
        } else if backup.Exists {
            if err := os.Remove(backup.Path); err != nil { return fail(err) }
        }
    }
    if matches, err := installBackupMatchesCurrent(stateBackup); err != nil || !matches {
        return fail(fmt.Errorf("Codex ownership state changed before retire commit: %v", err))
    }
    if err := os.Remove(statePath(codexDir)); err != nil { return fail(err) }
    if err := removeInstallTransactionJournal(codexDir, "finish Codex retirement"); err != nil { return err }

    dirs := make(map[string]bool)
    for _, record := range state.Files {
        dir := filepath.Dir(filepath.Join(codexDir, filepath.FromSlash(record.Path)))
        for dir != codexDir && dir != "." && dir != string(filepath.Separator) {
            dirs[dir] = true
            dir = filepath.Dir(dir)
        }
    }
    dirs[filepath.Dir(statePath(codexDir))] = true
    sorted := make([]string, 0, len(dirs))
    for dir := range dirs { sorted = append(sorted, dir) }
    sort.Slice(sorted, func(i,j int) bool { return len(sorted[i]) > len(sorted[j]) })
    for _, dir := range sorted {
        if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) &&
            !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) { return err }
    }
    _, _ = fmt.Fprintf(output, "Codex: retired %d owned instruction/rule/prompt records and managed config assignment\n", len(state.Files))
    return nil
}

func backupsAt(backups []installBackup, path string) installBackup {
    for _, backup := range backups {
        if backup.Path == path { return backup }
    }
    return installBackup{}
}
