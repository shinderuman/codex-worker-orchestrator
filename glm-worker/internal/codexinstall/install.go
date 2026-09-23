package codexinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type installPreparation struct {
	codexDir    string
	filePlan    fileInstallPlan
	configPlan  configInstallPlan
	state       installState
	stateExists bool
	stateBackup installBackup
}

func Install(repoRoot, codexDir string, stdout io.Writer) error {
	repoRoot = filepath.Clean(repoRoot)
	codexDir = filepath.Clean(codexDir)
	if repoRoot == "." || codexDir == "." {
		return fmt.Errorf("repo root and Codex directory must be explicit paths")
	}
	lock, err := acquireInstallLock(codexDir)
	if err != nil {
		return err
	}
	preparation, prepareErr := prepareInstall(repoRoot, codexDir)
	if prepareErr != nil {
		return joinInstallLockError(prepareErr, lock.Close())
	}
	installErr := applyInstall(preparation, stdout)
	return joinInstallLockError(installErr, lock.Close())
}

func prepareInstall(repoRoot, codexDir string) (installPreparation, error) {
	repoRoot = filepath.Clean(repoRoot)
	codexDir = filepath.Clean(codexDir)
	if repoRoot == "." || codexDir == "." {
		return installPreparation{}, fmt.Errorf("repo root and Codex directory must be explicit paths")
	}
	state, stateExists, stateBackup, err := loadInstallStateSnapshot(codexDir)
	if err != nil {
		return installPreparation{}, err
	}
	filePlan, err := buildFileInstallPlan(repoRoot, codexDir, state, stateExists)
	if err != nil {
		return installPreparation{}, err
	}
	configPlan, err := buildConfigInstallPlan(repoRoot, codexDir, state)
	if err != nil {
		return installPreparation{}, err
	}
	return installPreparation{
		codexDir: codexDir, filePlan: filePlan, configPlan: configPlan,
		state: state, stateExists: stateExists, stateBackup: stateBackup,
	}, nil
}

func applyInstall(preparation installPreparation, stdout io.Writer) error {
	return applyInstallWithStateWriter(preparation, stdout, writeState)
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
