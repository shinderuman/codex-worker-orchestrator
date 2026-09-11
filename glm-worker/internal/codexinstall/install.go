package codexinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Install(repoRoot, codexDir string, stdout io.Writer) error {
	repoRoot = filepath.Clean(repoRoot)
	codexDir = filepath.Clean(codexDir)
	if repoRoot == "." || codexDir == "." {
		return fmt.Errorf("repo root and Codex directory must be explicit paths")
	}

	state, stateExists, err := loadState(codexDir)
	if err != nil {
		return err
	}
	legacy := legacyManifest{Paths: map[string]bool{}}
	if !stateExists {
		legacy, err = loadLegacyManifest(codexDir)
		if err != nil {
			return err
		}
	}

	filePlan, err := buildFileInstallPlan(repoRoot, codexDir, state, stateExists, legacy)
	if err != nil {
		return err
	}
	configPlan, err := buildConfigInstallPlan(repoRoot, codexDir, state)
	if err != nil {
		return err
	}

	output := func(format string, args ...any) {
		_, _ = fmt.Fprintf(stdout, format, args...)
	}
	files, err := applyFileInstallPlan(codexDir, filePlan, output)
	if err != nil {
		return err
	}
	if err := applyConfigInstallPlan(configPlan, output); err != nil {
		return err
	}

	next := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
	if configPlan.Record != nil {
		next.Config[managedConfigKey] = *configPlan.Record
	}
	if err := writeState(codexDir, next); err != nil {
		return err
	}
	if !stateExists {
		if err := removeLegacyManifest(codexDir, legacy.Present); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".codex-worker-orchestrator-install-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
