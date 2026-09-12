package cliinstall

import (
	"fmt"
	"path/filepath"
)

func Verify(binDir string) error {
	if binDir == "" {
		return fmt.Errorf("binary directory is required")
	}
	state, err := loadState(ownershipStatePath(binDir))
	if err != nil {
		return err
	}
	for _, name := range managedNames {
		target := filepath.Join(binDir, name)
		info, exists, err := lstat(target)
		if err != nil {
			return err
		}
		if !exists || !isRegularExecutable(info) {
			return fmt.Errorf("repository CLI is missing or not executable: %s", target)
		}
		if digest, owned := state.Binaries[name]; owned {
			if err := requireOwnedTarget(target, info, digest); err != nil {
				return err
			}
		}
	}
	return nil
}
