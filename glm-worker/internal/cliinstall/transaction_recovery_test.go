package cliinstall

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInstallRecoversInterruptedUpgrade(t *testing.T) {
	if os.Getenv("CLI_INSTALL_RECOVERY_CHILD") == "1" {
		buildDir := os.Getenv("CLI_INSTALL_RECOVERY_BUILD")
		binDir := os.Getenv("CLI_INSTALL_RECOVERY_BIN")
		statePath := ownershipStatePath(binDir)
		state, err := loadState(statePath)
		if err != nil {
			t.Fatal(err)
		}
		actions, nextState, err := planInstall(buildDir, binDir, state)
		if err != nil {
			t.Fatal(err)
		}
		err = applyInstallWithStateWriter(actions, statePath, nextState, func(_, _ string) error {
			os.Exit(77)
			return nil
		})
		t.Fatalf("child install unexpectedly returned: %v", err)
	}

	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	writeBuildSet(t, buildDir, "v2")

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIInstallRecoversInterruptedUpgrade$")
	cmd.Env = append(os.Environ(),
		"CLI_INSTALL_RECOVERY_CHILD=1",
		"CLI_INSTALL_RECOVERY_BUILD="+buildDir,
		"CLI_INSTALL_RECOVERY_BIN="+binDir,
	)
	output, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 77 {
		t.Fatalf("child exit = %v, output = %s", err, output)
	}

	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatalf("Install() after interruption: %v", err)
	}
	assertAllStatus(t, results, "upgraded")
	assertCLIInstallVersion(t, binDir, "v2")
	assertCLIInstallTransactionAbsent(t, binDir)
}

func TestCLIInstallRecoversPartialBinaryPrefixBeforeRetry(t *testing.T) {
	buildDir, binDir := prepareOwnedCLIInstall(t)
	writeBuildSet(t, buildDir, "v2")
	journal, staged := stageCLIInstallTransactionForTest(t, buildDir, binDir)

	first := staged[0]
	if err := os.Rename(first.replacement, first.action.target); err != nil {
		t.Fatal(err)
	}

	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatalf("Install() after partial prefix: %v", err)
	}
	assertAllStatus(t, results, "upgraded")
	assertCLIInstallVersion(t, binDir, "v2")
	assertCLIInstallTransactionAbsent(t, binDir)
	for _, binary := range journal.Binaries {
		if binary.Backup != "" {
			if _, err := os.Lstat(binary.Backup); !os.IsNotExist(err) {
				t.Fatalf("backup remains after recovery: %s: %v", binary.Backup, err)
			}
		}
	}
}

func TestCLIInstallRecoveryPreservesExternalEditAfterInterruption(t *testing.T) {
	buildDir, binDir := prepareOwnedCLIInstall(t)
	writeBuildSet(t, buildDir, "v2")
	_, staged := stageCLIInstallTransactionForTest(t, buildDir, binDir)

	first := staged[0]
	if err := os.Rename(first.replacement, first.action.target); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, first.action.target, "user-edit-after-interruption")

	_, err := Install(buildDir, binDir)
	if err == nil || !strings.Contains(err.Error(), "matches neither preimage nor postimage") {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, first.action.target); got != "user-edit-after-interruption" {
		t.Fatalf("external edit was overwritten: %q", got)
	}
	if _, err := os.Lstat(cliInstallTransactionPath(binDir)); err != nil {
		t.Fatalf("transaction evidence was removed after ambiguous recovery: %v", err)
	}
}

func TestCLIInstallRecoveryRecognizesCommittedTransaction(t *testing.T) {
	buildDir, binDir := prepareOwnedCLIInstall(t)
	writeBuildSet(t, buildDir, "v2")
	journal, staged := stageCLIInstallTransactionForTest(t, buildDir, binDir)

	if _, err := commitActions(staged); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(journal.State.Temp, journal.State.Path); err != nil {
		t.Fatal(err)
	}

	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatalf("Install() after committed journal: %v", err)
	}
	assertAllStatus(t, results, "unchanged")
	assertCLIInstallVersion(t, binDir, "v2")
	assertCLIInstallTransactionAbsent(t, binDir)
}

func TestCLIInstallRecoveryFailsClosedOnCorruptJournal(t *testing.T) {
	buildDir, binDir := prepareOwnedCLIInstall(t)
	journalPath := cliInstallTransactionPath(binDir)
	if err := os.WriteFile(journalPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Install(buildDir, binDir)
	if err == nil || !strings.Contains(err.Error(), "decode CLI install transaction journal") {
		t.Fatalf("Install() error = %v", err)
	}
	assertCLIInstallVersion(t, binDir, "v1")
}

func TestCLIInstallStateWriteFailureUsesOrdinaryRollback(t *testing.T) {
	buildDir, binDir := prepareOwnedCLIInstall(t)
	writeBuildSet(t, buildDir, "v2")
	statePath := ownershipStatePath(binDir)
	state, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	actions, nextState, err := planInstall(buildDir, binDir, state)
	if err != nil {
		t.Fatal(err)
	}

	err = applyInstallWithStateWriter(actions, statePath, nextState, func(_, _ string) error {
		return errors.New("injected state failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected state failure") {
		t.Fatalf("applyInstallWithStateWriter() error = %v", err)
	}
	assertCLIInstallVersion(t, binDir, "v1")
	assertCLIInstallTransactionAbsent(t, binDir)
}

func stageCLIInstallTransactionForTest(
	t *testing.T,
	buildDir string,
	binDir string,
) (cliInstallTransactionJournal, []stagedAction) {
	t.Helper()
	statePath := ownershipStatePath(binDir)
	state, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	actions, nextState, err := planInstall(buildDir, binDir, state)
	if err != nil {
		t.Fatal(err)
	}
	stateTemp, err := stageState(filepath.Dir(statePath), nextState)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := stageActions(changedActions(actions))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := newCLIInstallTransactionJournal(statePath, stateTemp, staged)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveCLIInstallTransactionJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal, staged
}

func prepareOwnedCLIInstall(t *testing.T) (string, string) {
	t.Helper()
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	return buildDir, binDir
}

func assertCLIInstallVersion(t *testing.T, binDir, version string) {
	t.Helper()
	for _, name := range managedNames {
		if got := readFile(t, filepath.Join(binDir, name)); got != name+"-"+version {
			t.Fatalf("%s content = %q", name, got)
		}
	}
}

func assertCLIInstallTransactionAbsent(t *testing.T, binDir string) {
	t.Helper()
	if _, err := os.Lstat(cliInstallTransactionPath(binDir)); !os.IsNotExist(err) {
		t.Fatalf("CLI install transaction journal remains: %v", err)
	}
}
