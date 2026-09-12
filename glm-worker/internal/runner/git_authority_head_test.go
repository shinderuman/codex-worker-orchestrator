package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitAuthoritySnapshotSupportsDetachedAndUnbornHead(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("detached", func(t *testing.T) {
		root := newGitAuthorityRepo(t)
		runGitAuthorityCommand(t, root, "checkout", "--detach", "--quiet")
		snapshot, err := captureGitAuthoritySnapshot(realGit, root)
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.active || snapshot.head == "" || snapshot.symbolicHead != "" {
			t.Fatalf("detached snapshot = %#v", snapshot)
		}
	})

	t.Run("unborn", func(t *testing.T) {
		root := t.TempDir()
		runGitAuthorityCommand(t, "", "init", "-q", root)
		snapshot, err := captureGitAuthoritySnapshot(realGit, root)
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.active || snapshot.head != "" || !strings.HasPrefix(snapshot.symbolicHead, "refs/heads/") {
			t.Fatalf("unborn snapshot = %#v", snapshot)
		}
	})
}

func TestGitAuthorityGuardFailsClosedOnBrokenHead(t *testing.T) {
	root := newGitAuthorityRepo(t)
	refsHeads := filepath.Join(root, ".git", "refs", "heads")
	if err := os.WriteFile(filepath.Join(refsHeads, "broken"), []byte("feedfacefeedfacefeedfacefeedfacefeedface\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareGitAuthorityGuard(root); err == nil {
		t.Fatal("before-call guard accepted broken HEAD authority")
	} else {
		var guardErr *GitAuthorityGuardError
		if !errors.As(err, &guardErr) || guardErr.Stage != "capture-before-call" {
			t.Fatalf("guard error = %#v", err)
		}
	}
}

func TestGitAuthorityGuardVerifyFailsClosedWhenHeadBecomesBroken(t *testing.T) {
	root := newGitAuthorityRepo(t)
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()

	refsHeads := filepath.Join(root, ".git", "refs", "heads")
	if err := os.WriteFile(filepath.Join(refsHeads, "broken"), []byte("feedfacefeedfacefeedfacefeedfacefeedface\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = guard.verify()
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "capture-after-call" {
		t.Fatalf("verify error = %#v", err)
	}
}
