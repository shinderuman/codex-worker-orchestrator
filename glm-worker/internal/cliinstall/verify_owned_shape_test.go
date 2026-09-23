package cliinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyRejectsOwnedNonRegularTarget(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "glm-worker")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err == nil {
		t.Fatal("owned CLI directory replacement was accepted")
	}
}

func TestVerifyRejectsOwnedSymlinkTarget(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range managedNames {
		writeVerifyCLI(t, filepath.Join(buildDir, name), "#!/bin/sh\nprintf '%s\\n' "+name+"\n")
	}
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "glm-worker")
	backup := filepath.Join(binDir, "glm-worker-real")
	if err := os.Rename(target, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backup, target); err != nil {
		t.Fatal(err)
	}
	if err := Verify(binDir); err == nil {
		t.Fatal("owned CLI symlink replacement was accepted")
	}
}
