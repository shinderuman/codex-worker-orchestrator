package codexinstall

import (
    "bytes"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestRetireOwnedCodexFilesAndConfigOnly(t *testing.T) {
    repo := initInstallFixtureRepo(t)
    dir := t.TempDir()
    userFile := filepath.Join(dir, "AGENTS.md")
    userConfig := filepath.Join(dir, "config.toml")
    writeTestFile(t, userFile, []byte("global user instruction\n"))
    writeTestFile(t, userConfig, []byte("local_key = \"keep\"\n"))
    runInstall(t, repo, dir)
    if err := Retire(dir, &bytes.Buffer{}); err != nil { t.Fatal(err) }
    assertFileBytes(t, userFile, []byte("global user instruction\n"))
    assertFileBytes(t, userConfig, []byte("local_key = \"keep\"\n"))
    if _, err := os.Stat(statePath(dir)); !os.IsNotExist(err) {
        t.Fatalf("install state not removed: %v", err)
    }
    if _, err := os.Stat(filepath.Join(dir, "instructions", "test.md")); !os.IsNotExist(err) {
        t.Fatalf("managed instruction remains: %v", err)
    }
    if err := Retire(dir, &bytes.Buffer{}); err != nil {
        t.Fatalf("repeated retire: %v", err)
    }
    runInstall(t, repo, dir)
}

func TestRetireCodexRefusesModifiedInstruction(t *testing.T) {
    repo := initInstallFixtureRepo(t)
    dir := t.TempDir()
    runInstall(t, repo, dir)
    target := filepath.Join(dir, "instructions", "test.md")
    writeTestFile(t, target, []byte("user edit\n"))
    var out bytes.Buffer
    if err := Retire(dir, &out); err == nil || !strings.Contains(err.Error(), "refusing modified") {
        t.Fatalf("modified managed instruction accepted: %v", err)
    }
    if _, err := os.Stat(statePath(dir)); err != nil {
        t.Fatalf("ownership record lost: %v", err)
    }
    assertFileBytes(t, target, []byte("user edit\n"))
    if !strings.Contains(string(readTestFile(t, filepath.Join(dir, "config.toml"))), managedConfigKey) {
        t.Fatal("config unexpectedly retired on instruction conflict")
    }
}

func TestRetireCodexRefusesModifiedConfigAndSymlink(t *testing.T) {
    repo := initInstallFixtureRepo(t)
    dir := t.TempDir()
    runInstall(t, repo, dir)
    path := filepath.Join(dir, "config.toml")
    current := string(readTestFile(t, path))
    writeTestFile(t, path, []byte(strings.Replace(current, managedConfigKey+" = 21600000", managedConfigKey+" = 99", 1)))
    if err := Retire(dir, &bytes.Buffer{}); err == nil {
        t.Fatal("modified Codex config key accepted")
    }
    if _, err := os.Stat(statePath(dir)); err != nil { t.Fatal(err) }

    path = filepath.Join(dir, "instructions", "test.md")
    backup := filepath.Join(t.TempDir(), "user.txt")
    if err := os.Remove(path); err != nil { t.Fatal(err) }
    writeTestFile(t, backup, []byte("foreign"))
    if err := os.Symlink(backup, path); err != nil { t.Fatal(err) }
    if err := Retire(dir, &bytes.Buffer{}); err == nil {
        t.Fatal("symlinked managed Codex file accepted")
    }
    assertFileBytes(t, backup, []byte("foreign"))
}
