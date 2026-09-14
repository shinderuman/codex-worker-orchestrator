package parentactioncmd

import "errors"

func copyGuardRepairChangesWithRollback(worktree, repoRoot string, changed []string) (func() error, error) {
	backups, err := captureGuardRepairFiles(repoRoot, changed)
	if err != nil {
		return nil, err
	}
	rollback := func() error { return restoreGuardRepairFiles(repoRoot, backups) }
	if err := copyGuardRepairChanges(worktree, repoRoot, changed); err != nil {
		return nil, errors.Join(err, rollback())
	}
	return rollback, nil
}
