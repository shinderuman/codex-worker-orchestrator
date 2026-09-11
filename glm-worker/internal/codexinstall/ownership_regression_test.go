package codexinstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRemovesUnmodifiedRetiredManagedFile(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)

	source := filepath.Join(repo, "codex", "instructions", "test.md")
	target := filepath.Join(codexDir, "instructions", "test.md")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	runInstall(t, repo, codexDir)

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("retired managed file remains: %v", err)
	}
	state := loadTestState(t, codexDir)
	for _, record := range state.Files {
		if record.Path == "instructions/test.md" {
			t.Fatalf("retired file remained tool-owned: %+v", record)
		}
	}
}

func TestInstallRetiresOwnedConfigKeyWithoutChangingUserConfig(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	configPath := filepath.Join(codexDir, "config.toml")
	writeTestFile(t, configPath, []byte("local_key = \"keep\"\n"))
	runInstall(t, repo, codexDir)

	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), nil)
	runInstall(t, repo, codexDir)

	content := readTestFile(t, configPath)
	if strings.Contains(string(content), managedConfigKey) {
		t.Fatalf("retired managed config key remains:\n%s", content)
	}
	if !bytes.Contains(content, []byte("local_key = \"keep\"")) {
		t.Fatalf("unrelated config was changed:\n%s", content)
	}
	state := loadTestState(t, codexDir)
	if _, owned := state.Config[managedConfigKey]; owned {
		t.Fatalf("retired config key remained tool-owned: %+v", state.Config)
	}
}

func TestInstallDoesNotClaimPreexistingEqualConfigValue(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	configPath := filepath.Join(codexDir, "config.toml")
	original := []byte(managedConfigKey + " = 21600000\nlocal_key = \"keep\"\n")
	writeTestFile(t, configPath, original)
	runInstall(t, repo, codexDir)

	state := loadTestState(t, codexDir)
	if _, owned := state.Config[managedConfigKey]; owned {
		t.Fatalf("preexisting equal config value was claimed: %+v", state.Config)
	}
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), nil)
	runInstall(t, repo, codexDir)
	assertFileBytes(t, configPath, original)
}

func TestInstallRollbackRestoresLegacyManifestAndSuppressesSuccessOutput(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	manifestPath := filepath.Join(codexDir, legacyManifestName)
	manifest := []byte("instructions/test.md\n")
	writeTestFile(t, manifestPath, manifest)
	target := filepath.Join(codexDir, "instructions", "test.md")
	installed := []byte("# tool instruction\n")
	writeTestFile(t, target, installed)
	configPath := filepath.Join(codexDir, "config.toml")
	config := []byte(managedConfigKey + " = 21600000\nlocal_key = \"keep\"\n")
	writeTestFile(t, configPath, config)

	preparation, err := prepareInstall(repo, codexDir)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	stateFailure := errors.New("state commit failed")
	err = applyInstallWithStateWriter(preparation, &stdout, func(string, installState) error { return stateFailure })
	if !errors.Is(err, stateFailure) {
		t.Fatalf("expected state failure, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("rolled-back install emitted success output: %q", stdout.String())
	}
	assertFileBytes(t, manifestPath, manifest)
	assertFileBytes(t, target, installed)
	assertFileBytes(t, configPath, config)
	if _, err := os.Stat(statePath(codexDir)); !os.IsNotExist(err) {
		t.Fatalf("state exists after rolled-back legacy migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexDir, "instructions", "codex-worker-orchestrator.md")); !os.IsNotExist(err) {
		t.Fatalf("new managed file remains after rollback: %v", err)
	}
}
