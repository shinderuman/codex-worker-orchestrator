package codexinstall

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type installRecoveryFixture struct {
	repo     string
	codexDir string
	prep     installPreparation
	journal  installTransactionJournal
}

func newInstallRecoveryFixture(t *testing.T) installRecoveryFixture {
	t.Helper()
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)
	writeTestFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# tool instruction v2\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))
	prep, err := prepareInstall(repo, codexDir)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := captureInstallBackups(prep)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := newInstallTransactionJournal(prep, backups, plannedInstallState(prep))
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInstallTransactionJournal(installTransactionPath(codexDir), journal); err != nil {
		t.Fatal(err)
	}
	return installRecoveryFixture{repo: repo, codexDir: codexDir, prep: prep, journal: journal}
}

func TestRecoverInterruptedInstallRestoresPartialMutationBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
	}{
		{name: "after managed file", paths: []string{"instructions/test.md"}},
		{name: "after config", paths: []string{"instructions/test.md", "config.toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newInstallRecoveryFixture(t)
			for _, path := range tt.paths {
				writeInstallTransactionSurface(t, fixture.codexDir, fixture.journal, path, false)
			}
			if err := recoverInterruptedInstall(fixture.codexDir); err != nil {
				t.Fatalf("recover: %v", err)
			}
			assertInstallTransactionImages(t, fixture.codexDir, fixture.journal, true)
			assertInstallTransactionJournalAbsent(t, fixture.codexDir)
		})
	}
}

func TestRecoverInterruptedInstallPreservesUserEdit(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	writeInstallTransactionSurface(t, fixture.codexDir, fixture.journal, "instructions/test.md", false)
	target := filepath.Join(fixture.codexDir, "instructions", "test.md")
	userEdit := append(readTestFile(t, target), []byte("# user edit after interruption\n")...)
	writeTestFile(t, target, userEdit)

	err := recoverInterruptedInstall(fixture.codexDir)
	if err == nil || !strings.Contains(err.Error(), "changed after interruption") {
		t.Fatalf("expected fail-closed user-edit conflict, got %v", err)
	}
	assertFileBytes(t, target, userEdit)
	if _, err := os.Stat(installTransactionPath(fixture.codexDir)); err != nil {
		t.Fatalf("journal was removed after unsafe recovery: %v", err)
	}
}

func TestRecoverInterruptedInstallRecognizesCommittedTransaction(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	for _, surface := range fixture.journal.Surfaces {
		writeInstallTransactionSurface(t, fixture.codexDir, fixture.journal, surface.Path, false)
	}

	if err := recoverInterruptedInstall(fixture.codexDir); err != nil {
		t.Fatalf("recover committed transaction: %v", err)
	}
	assertInstallTransactionImages(t, fixture.codexDir, fixture.journal, false)
	assertInstallTransactionJournalAbsent(t, fixture.codexDir)
}

func TestRecoverInterruptedInstallRejectsCorruptJournal(t *testing.T) {
	codexDir := t.TempDir()
	path := installTransactionPath(codexDir)
	if err := writeAtomic(path, []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterruptedInstall(codexDir); err == nil || !strings.Contains(err.Error(), "decode Codex install transaction journal") {
		t.Fatalf("expected corrupt journal rejection, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corrupt journal was removed: %v", err)
	}
}

func TestRecoverInterruptedInstallRejectsWrongDestination(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	fixture.journal.Destination = filepath.Join(t.TempDir(), "other-codex")
	if err := saveInstallTransactionJournal(installTransactionPath(fixture.codexDir), fixture.journal); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterruptedInstall(fixture.codexDir); err == nil || !strings.Contains(err.Error(), "destination mismatch") {
		t.Fatalf("expected destination mismatch, got %v", err)
	}
	if _, err := os.Stat(installTransactionPath(fixture.codexDir)); err != nil {
		t.Fatalf("wrong-destination journal was removed: %v", err)
	}
}

func TestRecoverInterruptedInstallRejectsSymlinkedJournal(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	journalPath := installTransactionPath(fixture.codexDir)
	journalData := append([]byte(nil), readTestFile(t, journalPath)...)
	outside := filepath.Join(t.TempDir(), "journal.json")
	writeTestFile(t, outside, journalData)
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, journalPath); err != nil {
		t.Fatal(err)
	}

	err := recoverInterruptedInstall(fixture.codexDir)
	if err == nil || !strings.Contains(err.Error(), "journal is not a regular file") {
		t.Fatalf("expected symlinked journal rejection, got %v", err)
	}
	assertFileBytes(t, outside, journalData)
	if info, err := os.Lstat(journalPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("journal symlink was changed: info=%v err=%v", info, err)
	}
}

func TestRecoverInterruptedInstallRejectsSymlinkedSurfaceAncestor(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	outside := t.TempDir()
	for _, surface := range fixture.journal.Surfaces {
		if !strings.HasPrefix(surface.Path, "instructions/") || !surface.Post.Exists {
			continue
		}
		relative := strings.TrimPrefix(surface.Path, "instructions/")
		target := filepath.Join(outside, filepath.FromSlash(relative))
		writeTestFile(t, target, surface.Post.Content)
		if err := os.Chmod(target, surface.Post.Mode.Perm()); err != nil {
			t.Fatal(err)
		}
	}
	instructions := filepath.Join(fixture.codexDir, "instructions")
	if err := os.RemoveAll(instructions); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, instructions); err != nil {
		t.Fatal(err)
	}

	err := recoverInterruptedInstall(fixture.codexDir)
	if err == nil || !strings.Contains(err.Error(), "ancestor is not a real directory") {
		t.Fatalf("expected symlinked ancestor rejection, got %v", err)
	}
	for _, surface := range fixture.journal.Surfaces {
		if !strings.HasPrefix(surface.Path, "instructions/") || !surface.Post.Exists {
			continue
		}
		relative := strings.TrimPrefix(surface.Path, "instructions/")
		assertFileBytes(t, filepath.Join(outside, filepath.FromSlash(relative)), surface.Post.Content)
	}
	if _, err := os.Stat(installTransactionPath(fixture.codexDir)); err != nil {
		t.Fatalf("journal was removed after unsafe recovery: %v", err)
	}
}

func TestInstallRecoversAfterStateCommitBoundaryCrash(t *testing.T) {
	if os.Getenv("CODEX_INSTALL_CRASH_HELPER") == "1" {
		repo := os.Getenv("CODEX_INSTALL_CRASH_REPO")
		codexDir := os.Getenv("CODEX_INSTALL_CRASH_DEST")
		prep, err := prepareInstall(repo, codexDir)
		if err != nil {
			os.Exit(70)
		}
		_ = applyInstallWithStateWriter(prep, io.Discard, func(string, installState) error {
			os.Exit(77)
			return nil
		})
		os.Exit(71)
	}

	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)
	oldState := append([]byte(nil), readTestFile(t, statePath(codexDir))...)
	newTarget := []byte("# tool instruction after crash\n")
	writeTestFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), newTarget)
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))

	command := exec.Command(os.Args[0], "-test.run=^TestInstallRecoversAfterStateCommitBoundaryCrash$")
	command.Env = append(os.Environ(),
		"CODEX_INSTALL_CRASH_HELPER=1",
		"CODEX_INSTALL_CRASH_REPO="+repo,
		"CODEX_INSTALL_CRASH_DEST="+codexDir,
	)
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 77 {
		t.Fatalf("crash helper exit = %v, want 77", err)
	}
	if _, err := os.Stat(installTransactionPath(codexDir)); err != nil {
		t.Fatalf("durable journal missing after crash: %v", err)
	}
	assertFileBytes(t, statePath(codexDir), oldState)
	assertFileBytes(t, filepath.Join(codexDir, "instructions", "test.md"), newTarget)

	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err != nil {
		t.Fatalf("normal install did not recover interrupted transaction: %v", err)
	}
	assertInstallTransactionJournalAbsent(t, codexDir)
	assertFileBytes(t, filepath.Join(codexDir, "instructions", "test.md"), newTarget)
	state := loadTestState(t, codexDir)
	records := stateFileMap(state)
	if records["instructions/test.md"].SHA256 != digestBytes(newTarget) {
		t.Fatalf("recovered install state did not commit current target: %+v", records["instructions/test.md"])
	}
}

func TestFailedInstallRollbackRemovesTransactionJournal(t *testing.T) {
	fixture := newInstallRecoveryFixture(t)
	if err := removeInstallTransactionJournal(fixture.codexDir, "reset fixture journal"); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("state commit failed")
	err := applyInstallWithStateWriter(fixture.prep, io.Discard, func(string, installState) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("expected state failure, got %v", err)
	}
	assertInstallTransactionJournalAbsent(t, fixture.codexDir)
	assertInstallTransactionImages(t, fixture.codexDir, fixture.journal, true)
}

func writeInstallTransactionSurface(t *testing.T, codexDir string, journal installTransactionJournal, path string, pre bool) {
	t.Helper()
	for _, surface := range journal.Surfaces {
		if surface.Path != path {
			continue
		}
		image := surface.Post
		if pre {
			image = surface.Pre
		}
		if err := restoreInstallBackup(installTransactionImageBackup(codexDir, surface.Path, image)); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("transaction surface %q not found", path)
}

func assertInstallTransactionImages(t *testing.T, codexDir string, journal installTransactionJournal, pre bool) {
	t.Helper()
	for _, surface := range journal.Surfaces {
		current, err := captureInstallBackup(filepath.Join(codexDir, filepath.FromSlash(surface.Path)))
		if err != nil {
			t.Fatal(err)
		}
		expected := surface.Post
		if pre {
			expected = surface.Pre
		}
		if !installTransactionImageMatches(current, expected) {
			t.Fatalf("surface %s does not match expected image", surface.Path)
		}
	}
}

func assertInstallTransactionJournalAbsent(t *testing.T, codexDir string) {
	t.Helper()
	if _, err := os.Stat(installTransactionPath(codexDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install transaction journal still exists: %v", err)
	}
}
