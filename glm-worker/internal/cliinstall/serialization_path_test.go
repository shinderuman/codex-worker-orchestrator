package cliinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeBinDirMakesRelativePathAbsoluteBeforeAuthorityUse(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	got, err := normalizeBinDir(filepath.Join("relative", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workingDir, "relative", "bin")
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("normalized binDir = %q want %q", got, want)
	}
}
