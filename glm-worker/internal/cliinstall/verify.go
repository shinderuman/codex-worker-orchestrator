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
		expected, ok := state.ExpectedBinaries[name]
		if !ok {
			return fmt.Errorf("repository CLI expected identity is missing: %s", name)
		}
		target := filepath.Join(binDir, name)
		info, exists, err := lstat(target)
		if err != nil {
			return err
		}
		if !exists || !isRegularExecutable(info) {
			return fmt.Errorf("repository CLI is missing or not executable: %s", target)
		}
		observed, err := hashFile(target)
		if err != nil {
			return err
		}
		if observed != expected {
			return fmt.Errorf("repository CLI does not match the last successful install: %s", target)
		}
	}
	return nil
}
