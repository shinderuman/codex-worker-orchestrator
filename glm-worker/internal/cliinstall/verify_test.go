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

func TestVerifyRequiresAllCanonicalCommandsWithoutClaimingUnownedOwnership(t *testing.T) {
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
	if err := Verify(binDir); err != nil {
		t.Fatalf("valid unowned identical CLI rejected: %v", err)
	}
	if err := os.Remove(filepath.Join(binDir, "harnesslint")); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err == nil {
		t.Fatal("missing canonical unowned CLI was accepted")
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
