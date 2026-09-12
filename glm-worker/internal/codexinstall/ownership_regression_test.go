package codexinstall

import (
	"bytes"
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

func TestInstallRecognizesQuotedUserOwnedConfigKey(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	configPath := filepath.Join(codexDir, "config.toml")
	original := []byte("\"" + managedConfigKey + "\" = 5\nlocal_key = \"keep\"\n")
	writeTestFile(t, configPath, original)

	var stdout bytes.Buffer
	err := Install(repo, codexDir, &stdout)
	if err == nil || !strings.Contains(err.Error(), "user-owned Codex config key") {
		t.Fatalf("expected quoted user-owned key conflict, got %v", err)
	}
	assertFileBytes(t, configPath, original)
	if _, err := os.Stat(statePath(codexDir)); !os.IsNotExist(err) {
		t.Fatalf("state exists after quoted config ownership conflict: %v", err)
	}
}
