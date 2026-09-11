package codexinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type installPreparation struct {
	codexDir    string
	stateExists bool
	legacy      legacyManifest
	filePlan    fileInstallPlan
	configPlan  configInstallPlan
}

func Install(repoRoot, codexDir string, stdout io.Writer) error {
	preparation, err := prepareInstall(repoRoot, codexDir)
	if err != nil {
		return err
	}
	return applyInstall(preparation, stdout)
}

func prepareInstall(repoRoot, codexDir string) (installPreparation, error) {
	repoRoot = filepath.Clean(repoRoot)
	codexDir = filepath.Clean(codexDir)
	if repoRoot == "." || codexDir == "." {
		return installPreparation{}, fmt.Errorf("repo root and Codex directory must be explicit paths")
	}
	state, stateExists, err := loadState(codexDir)
	if err != nil {
		return installPreparation{}, err
	}
	legacy, err := legacyForInstall(codexDir, stateExists)
	if err != nil {
		return installPreparation{}, err
	}
	filePlan, err := buildFileInstallPlan(repoRoot, codexDir, state, stateExists, legacy)
	if err != nil {
		return installPreparation{}, err
	}
	configPlan, err := buildConfigInstallPlan(repoRoot, codexDir, state, stateExists, legacy)
	if err != nil {
		return installPreparation{}, err
	}
	return installPreparation{codexDir: codexDir, stateExists: stateExists, legacy: legacy, filePlan: filePlan, configPlan: configPlan}, nil
}

func legacyForInstall(codexDir string, stateExists bool) (legacyManifest, error) {
	if stateExists {
		return legacyManifest{Paths: map[string]bool{}}, nil
	}
	return loadLegacyManifest(codexDir)
}

func applyInstall(preparation installPreparation, stdout io.Writer) error {
	output := func(format string, args ...any) {
		_, _ = fmt.Fprintf(stdout, format, args...)
	}
	files, err := applyFileInstallPlan(preparation.codexDir, preparation.filePlan, output)
	if err != nil {
		return err
	}
	if err := applyConfigInstallPlan(preparation.configPlan, output); err != nil {
		return err
	}
	next := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
	if preparation.configPlan.Record != nil {
		next.Config[managedConfigKey] = *preparation.configPlan.Record
	}
	if err := writeState(preparation.codexDir, next); err != nil {
		return err
	}
	if preparation.stateExists {
		return nil
	}
	return removeLegacyManifest(preparation.codexDir, preparation.legacy.Present)
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
