//go:build linux || darwin

package parentevidence

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiffReplacedRegularLeafRejectsWithoutFollowingOrBlocking(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			repo, _ := newReviewCoverageStore(t)
			leaf := filepath.Join(repo, "review.go")
			parent, err := diffOpenParent(repo, "review.go")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()
			var before unix.Stat_t
			if err := unix.Fstatat(int(parent.Fd()), "review.go", &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(leaf); err != nil {
				t.Fatal(err)
			}
			if kind == "fifo" {
				err = unix.Mkfifo(leaf, 0o600)
			} else {
				err = os.Symlink(filepath.Join(t.TempDir(), "outside"), leaf)
			}
			if err != nil {
				t.Fatal(err)
			}
			if body, err := diffReadRegularLeaf(parent, "review.go", before); err == nil || len(body) != 0 {
				t.Fatalf("replaced leaf read: %q %v", body, err)
			}
		})
	}
}

func TestDiffPatchUsesCapturedContentAfterWorktreeReplacement(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	headMode, headBlob, err := gitDiffEntry(repo, "review.go", false)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("package review\nvar captured = true\n")
	file := DiffFile{Path: "review.go", HeadMode: headMode, HeadBlob: headBlob, WorktreeMode: "100644"}
	if err := os.Remove(filepath.Join(repo, "review.go")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(repo, "review.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := capturedDiffPatch(repo, file, content, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+var captured = true") {
		t.Fatalf("captured content missing: %q", patch)
	}
	projector := NewProjector(repo, st, "fifo-refused", Providers{})
	projector.Project(identityAdmissionManifest("review.go"))
	if projector.Output().Parts[0].Status != PartError {
		t.Fatal("FIFO evidence was accepted")
	}
	if err := projector.Commit(&bytes.Buffer{}, "reject FIFO"); err != nil {
		t.Fatal(err)
	}
}
