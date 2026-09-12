package cliinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyChecksCompleteOwnedCLISurface(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err != nil {
		t.Fatalf("matching CLI installation rejected: %v", err)
	}

	if err := os.WriteFile(filepath.Join(binDir, "commentlint"), []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err == nil {
		t.Fatal("owned CLI drift was accepted")
	}
}

func TestVerifyChecksUnownedCLIIdentityWithoutClaimingOwnership(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		content := "#!/bin/sh\nprintf '%s\\n' " + name + "\n"
		writeVerifyCLI(t, filepath.Join(buildDir, name), content)
		if name == "harnesslint" {
			writeVerifyCLI(t, filepath.Join(binDir, name), content)
		}
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(ownershipStatePath(binDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := state.Binaries["harnesslint"]; owned {
		t.Fatal("byte-identical preexisting harnesslint was claimed as installer-owned")
	}
	if _, recorded := state.ExpectedBinaries["harnesslint"]; !recorded {
		t.Fatal("unowned harnesslint expected identity was not recorded")
	}
	if err := Verify(binDir); err != nil {
		t.Fatalf("valid unowned identical CLI rejected: %v", err)
	}

	writeVerifyCLI(t, filepath.Join(binDir, "harnesslint"), "#!/bin/sh\nexit 9\n")
	if err := Verify(binDir); err == nil {
		t.Fatal("drifted canonical unowned CLI was accepted")
	}
}

func TestInstallRecordsExpectedIdentityWhenAllCLIsRemainUnowned(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		content := "#!/bin/sh\nprintf '%s\\n' " + name + "\n"
		writeVerifyCLI(t, filepath.Join(buildDir, name), content)
		writeVerifyCLI(t, filepath.Join(binDir, name), content)
	}

	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != "unchanged-unowned" {
			t.Fatalf("%s status = %q", result.Name, result.Status)
		}
	}
	state, err := loadState(ownershipStatePath(binDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Binaries) != 0 {
		t.Fatalf("unowned binaries were claimed: %#v", state.Binaries)
	}
	if len(state.ExpectedBinaries) != len(managedNames) {
		t.Fatalf("expected identity entries = %d, want %d", len(state.ExpectedBinaries), len(managedNames))
	}
	if err := Verify(binDir); err != nil {
		t.Fatalf("expected-only CLI installation rejected: %v", err)
	}

	if _, err := Retire(binDir); err != nil {
		t.Fatal(err)
	}
	for _, name := range managedNames {
		if _, err := os.Stat(filepath.Join(binDir, name)); err != nil {
			t.Fatalf("unowned CLI %s was removed: %v", name, err)
		}
	}
	if _, err := os.Stat(ownershipStatePath(binDir)); !os.IsNotExist(err) {
		t.Fatalf("expected-only state remained after retire: %v", err)
	}
}

func TestVerifyRequiresAllCanonicalCommands(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(binDir, "harnesslint")); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err == nil {
		t.Fatal("missing canonical CLI was accepted")
	}
}

func TestVerifyRejectsStateWithoutExpectedIdentity(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(ownershipStatePath(binDir))
	if err != nil {
		t.Fatal(err)
	}
	delete(state.ExpectedBinaries, "commentlint")
	writeVerifyState(t, binDir, state)
	if err := Verify(binDir); err == nil {
		t.Fatal("missing expected CLI identity was accepted")
	}
}

func TestVerifyRejectsOwnedIdentityStateThatDiffersFromExpectedInstall(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(ownershipStatePath(binDir))
	if err != nil {
		t.Fatal(err)
	}
	state.Binaries["commentlint"] = state.ExpectedBinaries["harnesslint"]
	writeVerifyState(t, binDir, state)
	if err := Verify(binDir); err == nil {
		t.Fatal("stale owned CLI identity state was accepted")
	}
}

func writeVerifyState(t *testing.T, binDir string, state installState) {
	t.Helper()
	statePath := ownershipStatePath(binDir)
	stateTemp, err := stageState(filepath.Dir(statePath), state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stateTemp, statePath); err != nil {
		t.Fatal(err)
	}
}

func writeVerifyCLI(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
