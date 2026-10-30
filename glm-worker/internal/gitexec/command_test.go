package gitexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentWorktreeScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, ".git"), []byte("gitdir: unused\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inherited := []string{"GIT_INDEX_FILE=.git/alternate", "GIT_DIR=.git", "GIT_WORK_TREE=.", "GIT_OBJECT_DIRECTORY=.git/objects", "GIT_CONFIG_COUNT=1", "PATH=" + os.Getenv("PATH"), "KEEP=value"}
	same, err := environment(nested, root, inherited)
	if err != nil || !reflect.DeepEqual(same, inherited) {
		t.Fatalf("same worktree environment: %v %v", same, err)
	}
	clean, err := environment(root, foreign, inherited)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range clean {
		if strings.HasPrefix(item, "GIT_") {
			t.Fatalf("foreign Git environment retained: %s", item)
		}
	}
	if !reflect.DeepEqual(clean, inherited[len(inherited)-2:]) {
		t.Fatalf("unrelated environment changed: %v", clean)
	}
}

func TestCommandPreservesAlternateIndexAndIsolatesForeignWorktree(t *testing.T) {
	root := t.TempDir()
	runGit := func(dir string, args ...string) {
		t.Helper()
		if output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit(root, "init", "-q")
	runGit(root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	foreign := filepath.Join(t.TempDir(), "lane")
	runGit(root, "worktree", "add", "--detach", "-q", foreign)
	origin, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origin); err != nil {
			t.Error(err)
		}
	})
	alternate := filepath.Join(root, ".git", "alternate")
	t.Setenv("GIT_INDEX_FILE", alternate)
	runGit(root, "read-tree", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "only-primary"), []byte("primary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(root, "add", "only-primary")
	output, err := Command("git", root, "ls-files").Output()
	if err != nil || strings.TrimSpace(string(output)) != "only-primary" {
		t.Fatalf("alternate index lost: %s %v", output, err)
	}
	for _, index := range []string{alternate, ".git/alternate"} {
		t.Setenv("GIT_INDEX_FILE", index)
		t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
		t.Setenv("GIT_WORK_TREE", root)
		output, err = Command("git", foreign, "ls-files").Output()
		if err != nil || len(output) != 0 {
			t.Fatalf("foreign index/worktree polluted for %s: %s %v", index, output, err)
		}
	}
}
