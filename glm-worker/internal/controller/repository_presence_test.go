package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryPresentRejectsNonGitPathWithoutResolvingIdentity(t *testing.T) {
	root := t.TempDir()
	present, err := RepositoryPresent(root)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("non-Git path was treated as controller-applicable repository")
	}
}

func TestRepositoryPresentAcceptsPrimaryAndLinkedWorktrees(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	for _, root := range []string{repo, linked, filepath.Join(linked, "nested")} {
		if root != repo && root != linked {
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		present, err := RepositoryPresent(root)
		if err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Fatalf("Git worktree was not controller-applicable: %s", root)
		}
	}
}
