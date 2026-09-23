package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualityGateWorkingDirIdentityResolvesSymlinks(t *testing.T) {
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "module-link")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if !sameQualityGateWorkingDir(target, alias) {
		t.Fatalf("same working directory via symlink was treated as distinct: target=%s alias=%s", target, alias)
	}
	if sameQualityGateWorkingDir(target, filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("unresolvable working directory was accepted as the same identity")
	}
}
