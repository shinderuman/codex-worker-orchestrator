package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepositoryPathsIgnoresTrackedFileDeletedFromWorkingTree(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init")
	path := filepath.Join(root, "deleted.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "deleted.go")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	paths, err := repositoryPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("paths = %v", paths)
	}
}
