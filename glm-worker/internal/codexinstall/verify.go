package codexinstall

import (
	"fmt"
	"os"
	"path/filepath"
)

// Verify checks that the current repository-managed Codex surface matches the
// canonical installer ownership state without taking ownership of preserved
// user content.
func Verify(repoRoot, codexDir string) error {
	repoRoot = filepath.Clean(repoRoot)
	codexDir = filepath.Clean(codexDir)
	if repoRoot == "." || codexDir == "." {
		return fmt.Errorf("repo root and Codex directory must be explicit paths")
	}
	state, stateExists, err := loadState(codexDir)
	if err != nil {
		return err
	}
	if !stateExists {
		return fmt.Errorf("Codex install ownership state is missing")
	}
	desired, err := collectDesiredFiles(repoRoot)
	if err != nil {
		return err
	}
	if err := verifyInstalledFiles(codexDir, desired, state); err != nil {
		return err
	}
	return verifyInstalledConfig(repoRoot, codexDir, state)
}

func verifyInstalledFiles(codexDir string, desired []desiredFile, state installState) error {
	records := stateFileMap(state)
	for _, file := range desired {
		record, owned := records[file.Path]
		if !owned {
			return fmt.Errorf("managed Codex file is missing ownership state: %s", file.Path)
		}
		if record.SHA256 != file.SHA256 {
			return fmt.Errorf("managed Codex ownership state is stale for source: %s", file.Path)
		}
		target := filepath.Join(codexDir, filepath.FromSlash(file.Path))
		actual, err := digestRegularFile(target)
		if err != nil {
			return fmt.Errorf("verify installed Codex file %s: %w", file.Path, err)
		}
		if actual != file.SHA256 {
			return fmt.Errorf("installed Codex file does not match source: %s", file.Path)
		}
		delete(records, file.Path)
	}
	for path := range records {
		return fmt.Errorf("Codex ownership state retains retired managed file: %s", path)
	}
	return nil
}

func verifyInstalledConfig(repoRoot, codexDir string, state installState) error {
	managedData, err := os.ReadFile(filepath.Join(repoRoot, "codex", "config-managed.toml"))
	if err != nil {
		return fmt.Errorf("read managed Codex config: %w", err)
	}
	managed, managedFound, err := findTopLevelAssignment(managedData, managedConfigKey)
	if err != nil {
		return fmt.Errorf("managed Codex config: %w", err)
	}
	installedData, _, err := readOptionalFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		return err
	}
	installed, installedFound, err := findTopLevelAssignment(installedData, managedConfigKey)
	if err != nil {
		return fmt.Errorf("installed Codex config: %w", err)
	}
	record, owned := state.Config[managedConfigKey]
	if !managedFound {
		if owned {
			return fmt.Errorf("Codex ownership state retains retired managed config key: %s", managedConfigKey)
		}
		return nil
	}
	if !installedFound || installed.Value != managed.Value {
		return fmt.Errorf("installed Codex config does not match managed value: %s", managedConfigKey)
	}
	if !owned {
		return nil
	}
	if record.Value != managed.Value || record.LineSHA256 != digestBytes([]byte(installed.Line)) {
		return fmt.Errorf("managed Codex config ownership state does not match installation: %s", managedConfigKey)
	}
	return nil
}
