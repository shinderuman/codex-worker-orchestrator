package codexinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsPreexistingUserManagedPathBeforeMutation(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	configPath := filepath.Join(codexDir, "config.toml")
	originalConfig := []byte("local_key = \"keep\"\n")
	writeTestFile(t, configPath, originalConfig)
	userPath := filepath.Join(codexDir, "instructions", "test.md")
	originalUser := []byte("# user instruction\n")
	writeTestFile(t, userPath, originalUser)

	var stdout bytes.Buffer
	err := Install(repo, codexDir, &stdout)
	if err == nil || !strings.Contains(err.Error(), "preexisting Codex file") {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
	assertFileBytes(t, userPath, originalUser)
	assertFileBytes(t, configPath, originalConfig)
	if _, err := os.Stat(statePath(codexDir)); !os.IsNotExist(err) {
		t.Fatalf("install state exists after rejected preflight: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexDir, "rules", "glm-worker.rules")); !os.IsNotExist(err) {
		t.Fatalf("managed file was written before preflight failure: %v", err)
	}
}

func TestInstallUpdatesOwnedFileAndPreservesModifiedObsoleteFile(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)

	source := filepath.Join(repo, "codex", "instructions", "test.md")
	target := filepath.Join(codexDir, "instructions", "test.md")
	writeTestFile(t, source, []byte("# tool instruction v2\n"))
	runInstall(t, repo, codexDir)
	assertFileBytes(t, target, []byte("# tool instruction v2\n"))

	userEdit := []byte("# user changed former tool file\n")
	writeTestFile(t, target, userEdit)
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	runInstall(t, repo, codexDir)
	assertFileBytes(t, target, userEdit)
	state := loadTestState(t, codexDir)
	for _, record := range state.Files {
		if record.Path == "instructions/test.md" {
			t.Fatalf("modified obsolete file remained tool-owned: %+v", record)
		}
	}
}

func TestInstallPreservesUnrelatedConfigAndRejectsUserOwnedManagedKey(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	configPath := filepath.Join(codexDir, "config.toml")
	writeTestFile(t, configPath, []byte("local_key = \"keep\"\n[features]\napps = true\n"))
	runInstall(t, repo, codexDir)
	content := readTestFile(t, configPath)
	if !bytes.Contains(content, []byte("local_key = \"keep\"")) || !bytes.Contains(content, []byte("apps = true")) || !bytes.Contains(content, []byte(managedConfigKey+" = 21600000")) {
		t.Fatalf("unexpected merged config:\n%s", content)
	}

	conflictDir := t.TempDir()
	conflictPath := filepath.Join(conflictDir, "config.toml")
	original := []byte(managedConfigKey + " = 5\nlocal_key = \"keep\"\n")
	writeTestFile(t, conflictPath, original)
	var stdout bytes.Buffer
	err := Install(repo, conflictDir, &stdout)
	if err == nil || !strings.Contains(err.Error(), "user-owned Codex config key") {
		t.Fatalf("expected user-owned key conflict, got %v", err)
	}
	assertFileBytes(t, conflictPath, original)
	if _, err := os.Stat(statePath(conflictDir)); !os.IsNotExist(err) {
		t.Fatalf("state exists after config ownership conflict: %v", err)
	}
}

func TestInstallRejectsEditedOwnedConfigKey(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)
	configPath := filepath.Join(codexDir, "config.toml")
	content := string(readTestFile(t, configPath))
	content = strings.Replace(content, managedConfigKey+" = 21600000", managedConfigKey+" = 123 # user edit", 1)
	writeTestFile(t, configPath, []byte(content))
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))

	var stdout bytes.Buffer
	err := Install(repo, codexDir, &stdout)
	if err == nil || !strings.Contains(err.Error(), "modified after install") {
		t.Fatalf("expected modified owned config conflict, got %v", err)
	}
	if !bytes.Contains(readTestFile(t, configPath), []byte("123 # user edit")) {
		t.Fatal("user config edit was overwritten")
	}
}

func TestInstallMigratesLegacyManifestOnlyWhenBytesMatchRepositoryHistory(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	legacyContent := []byte("# historical tool instruction\n")
	source := filepath.Join(repo, "codex", "instructions", "test.md")
	writeTestFile(t, source, legacyContent)
	commitFixtureRepo(t, repo, "historical instruction")
	writeTestFile(t, source, []byte("# current tool instruction\n"))
	commitFixtureRepo(t, repo, "current instruction")

	codexDir := t.TempDir()
	target := filepath.Join(codexDir, "instructions", "test.md")
	writeTestFile(t, target, legacyContent)
	writeTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("instructions/test.md\n"))
	writeTestFile(t, filepath.Join(codexDir, "config.toml"), []byte(managedConfigKey+" = 21600000\n"))
	runInstall(t, repo, codexDir)
	assertFileBytes(t, target, []byte("# current tool instruction\n"))
	if _, err := os.Stat(filepath.Join(codexDir, legacyManifestName)); !os.IsNotExist(err) {
		t.Fatalf("legacy manifest remains after migration: %v", err)
	}

	userDir := t.TempDir()
	userTarget := filepath.Join(userDir, "instructions", "test.md")
	userContent := []byte("# user replacement\n")
	writeTestFile(t, userTarget, userContent)
	writeTestFile(t, filepath.Join(userDir, legacyManifestName), []byte("instructions/test.md\n"))
	writeTestFile(t, filepath.Join(userDir, "config.toml"), []byte(managedConfigKey+" = 21600000\n"))
	var stdout bytes.Buffer
	err := Install(repo, userDir, &stdout)
	if err == nil {
		t.Fatal("expected legacy ownership conflict")
	}
	assertFileBytes(t, userTarget, userContent)
}

func TestInstallMigratesLegacyManagedConfigOwnership(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	managedPath := filepath.Join(repo, "codex", "config-managed.toml")
	writeTestFile(t, managedPath, []byte(managedConfigKey+" = 100\n"))
	commitFixtureRepo(t, repo, "legacy managed config")
	writeTestFile(t, managedPath, []byte(managedConfigKey+" = 200\n"))
	commitFixtureRepo(t, repo, "current managed config")

	codexDir := t.TempDir()
	writeTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("AGENTS.md\n"))
	configPath := filepath.Join(codexDir, "config.toml")
	writeTestFile(t, configPath, []byte(managedConfigKey+" = 100\nlocal_key = \"keep\"\n"))
	runInstall(t, repo, codexDir)
	if !bytes.Contains(readTestFile(t, configPath), []byte(managedConfigKey+" = 200")) {
		t.Fatal("legacy managed config was not upgraded")
	}
	state := loadTestState(t, codexDir)
	if state.Config[managedConfigKey].Value != "200" {
		t.Fatalf("legacy managed config ownership was not migrated: %+v", state.Config)
	}

	writeTestFile(t, managedPath, []byte(managedConfigKey+" = 300\n"))
	runInstall(t, repo, codexDir)
	if !bytes.Contains(readTestFile(t, configPath), []byte(managedConfigKey+" = 300")) {
		t.Fatal("migrated managed config was not updated")
	}
}

func TestInstallRejectsUnsafeLegacyManifestPath(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	writeTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("instructions/../../outside\n"))
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err == nil {
		t.Fatal("expected unsafe legacy manifest path rejection")
	}
}

func TestInstallRejectsSymlinkedUserConfig(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "config.toml")
	original := []byte("local_key = \"keep\"\n")
	writeTestFile(t, target, original)
	if err := os.Symlink(target, filepath.Join(codexDir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err == nil {
		t.Fatal("expected symlinked user config rejection")
	}
	assertFileBytes(t, target, original)
}

func TestInstallRejectsSymlinkedLegacyManifest(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "manifest")
	writeTestFile(t, target, []byte("instructions/test.md\n"))
	if err := os.Symlink(target, filepath.Join(codexDir, legacyManifestName)); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err == nil {
		t.Fatal("expected symlinked legacy manifest rejection")
	}
}

func TestInstallRejectsSymlinkedManagedAncestor(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(codexDir, "instructions")); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err == nil {
		t.Fatal("expected symlinked managed ancestor rejection")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("managed install escaped Codex directory: entries=%v err=%v", entries, err)
	}
}

func TestInstallRollsBackWhenStateCommitFails(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)
	target := filepath.Join(codexDir, "instructions", "test.md")
	configPath := filepath.Join(codexDir, "config.toml")
	stateFile := statePath(codexDir)
	oldTarget := append([]byte(nil), readTestFile(t, target)...)
	oldConfig := append([]byte(nil), readTestFile(t, configPath)...)
	oldState := append([]byte(nil), readTestFile(t, stateFile)...)

	writeTestFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# tool instruction changed\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))
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
	assertFileBytes(t, target, oldTarget)
	assertFileBytes(t, configPath, oldConfig)
	assertFileBytes(t, stateFile, oldState)
}

func runInstall(t *testing.T, repo, codexDir string) {
	t.Helper()
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err != nil {
		t.Fatalf("install: %v\n%s", err, stdout.String())
	}
}

func initInstallFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "codex", "AGENTS.md"), []byte("# parent rules\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# tool instruction\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "rules", "glm-worker.rules"), []byte("prefix_rule(pattern=[\"glm-worker\"], decision=\"allow\")\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "glm-worker", "prompts", "WORKER.md"), []byte("# worker\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600000\n"))
	command := exec.Command("git", "init", "-q", "-b", "main", repo)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	commitFixtureRepo(t, repo, "fixture")
	return repo
}

func commitFixtureRepo(t *testing.T, repo, message string) {
	t.Helper()
	commands := [][]string{
		{"add", "-A"},
		{"-c", "user.name=codexinstall-test", "-c", "user.email=codexinstall@example.invalid", "commit", "-qm", message},
	}
	for _, args := range commands {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	if actual := readTestFile(t, path); !bytes.Equal(actual, expected) {
		t.Fatalf("%s changed:\nwant %q\ngot  %q", path, expected, actual)
	}
}

func loadTestState(t *testing.T, codexDir string) installState {
	t.Helper()
	data := readTestFile(t, statePath(codexDir))
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
