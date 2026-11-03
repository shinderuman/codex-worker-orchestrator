package gitexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalVariablesCachesSuccessByGitPathAndRetriesFailure(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first-git")
	second := filepath.Join(t.TempDir(), "second-git")
	retrying := filepath.Join(t.TempDir(), "retrying-git")
	write := func(path, script string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(first, "printf 'GIT_DIR\\n'\n")
	write(second, "printf 'GIT_INDEX_FILE\\n'\n")
	write(retrying, "if [ ! -f \"$0.ready\" ]; then exit 23; fi\nprintf 'GIT_WORK_TREE\\n'\n")
	names, err := localVariables(first)
	if err != nil || !reflect.DeepEqual(names, map[string]bool{"GIT_DIR": true}) {
		t.Fatalf("first Git-local variables = %v err=%v", names, err)
	}
	names, err = localVariables(second)
	if err != nil || !reflect.DeepEqual(names, map[string]bool{"GIT_INDEX_FILE": true}) {
		t.Fatalf("second Git-local variables = %v err=%v", names, err)
	}
	write(first, "printf 'WRONG_AFTER_CACHE\\n'\n")
	names, err = localVariables(first)
	if err != nil || !reflect.DeepEqual(names, map[string]bool{"GIT_DIR": true}) {
		t.Fatalf("successful Git-local cache changed: %v err=%v", names, err)
	}
	if _, err := localVariables(retrying); err == nil {
		t.Fatal("failed Git-local lookup returned success")
	}
	if err := os.WriteFile(retrying+".ready", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	names, err = localVariables(retrying)
	if err != nil || !reflect.DeepEqual(names, map[string]bool{"GIT_WORK_TREE": true}) {
		t.Fatalf("transient Git-local lookup failure was cached: %v err=%v", names, err)
	}
}

func TestCommandExplicitGitPathDoesNotDependOnPATHGit(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	realGit, err = filepath.Abs(realGit)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if output, err := exec.Command(realGit, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "not-a-repository"))
	cmd := Command(realGit, repo, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "true\n" {
		t.Fatalf("explicit Git executable did not isolate local environment: %q %v", out, err)
	}
}

func TestCommandGitDirSeparatesInheritedGitEnvironment(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "init", "--bare", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "different-repository"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	cmd := CommandGitDir("git", repo, "rev-parse", "--is-bare-repository")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "true\n" {
		t.Fatalf("explicit --git-dir command used inherited Git location: %q %v", out, err)
	}
}
