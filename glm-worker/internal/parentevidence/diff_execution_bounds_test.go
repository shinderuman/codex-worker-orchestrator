//go:build linux || darwin

package parentevidence

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffSourceExecutionLimitRejectsLargeWorktreeBeforeTinyBudget(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "tracked regular", path: "review.go"},
		{name: "untracked binary", path: "large.bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, st := newReviewCoverageStore(t)
			path := filepath.Join(repo, tc.path)
			if err := os.WriteFile(path, bytes.Repeat([]byte{0, 0xff}, int(maxDiffSourceBytes/2)+1), 0o600); err != nil {
				t.Fatal(err)
			}
			projector := NewProjector(repo, st, "source-limit", Providers{})
			projector.Project(Manifest{
				Version: ManifestVersion,
				Reason: "bounded source",
				Diff: []DiffRequest{{Question: "large source", Paths: []string{tc.path}, BudgetBytes: 1}},
			})
			part := projector.Output().Parts[0]
			if part.Status != PartError || part.Digest != "" || part.Diff != nil ||
				!strings.Contains(part.Reason, tc.path) ||
				!strings.Contains(part.Reason, "source execution limit") ||
				!strings.Contains(part.Reason, "HEAD") || !strings.Contains(part.Reason, "index") {
				t.Fatalf("oversized worktree was accepted or lost provenance: %#v", part)
			}
			if err := projector.Commit(&bytes.Buffer{}, "bounded diff refused"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiffSourceExecutionLimitRejectsLargeHeadBlob(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	path := filepath.Join(repo, "large-head.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xff, 0}, int(maxDiffSourceBytes/2)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repo, "add", "--", "large-head.bin")
	gitReviewCoverageCommand(t, repo, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-qm", "large HEAD blob")
	_, headBlob, err := gitDiffEntry(repo, "large-head.bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("small changed worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repo, st, "head-limit", Providers{})
	projector.Project(Manifest{
		Version: ManifestVersion, Reason: "bounded HEAD",
		Diff: []DiffRequest{{Question: "large HEAD", Paths: []string{"large-head.bin"}, BudgetBytes: 1}},
	})
	part := projector.Output().Parts[0]
	if part.Status != PartError || part.Digest != "" ||
		!strings.Contains(part.Reason, "source execution limit") ||
		!strings.Contains(part.Reason, headBlob) {
		t.Fatalf("oversized HEAD was accepted or lost blob identity: %#v", part)
	}
}

func TestDiffAggregatePatchExecutionLimit(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	paths := []string{"one.txt", "two.txt", "three.txt"}
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(repo, path), []byte(strings.Repeat("a\n", 250*1024)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	projector := NewProjector(repo, st, "aggregate-limit", Providers{})
	projector.Project(Manifest{
		Version: ManifestVersion, Reason: "bounded aggregate",
		Diff: []DiffRequest{{Question: "aggregate", Paths: paths, BudgetBytes: 1}},
	})
	part := projector.Output().Parts[0]
	if part.Status != PartError || part.Digest != "" ||
		!strings.Contains(part.Reason, "aggregate patch execution limit") ||
		!strings.Contains(part.Reason, "three.txt") {
		t.Fatalf("unbounded aggregate patch or misleading success: %#v", part)
	}
}

func TestDiffTrackedBinaryPatchOutputLimit(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	file := filepath.Join(repo, "binary.bin")
	data := make([]byte, int(maxDiffSourceBytes)-4096)
	if _, err := rand.New(rand.NewSource(1)).Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repo, "add", "--", "binary.bin")
	gitReviewCoverageCommand(t, repo, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-qm", "binary HEAD")
	if _, err := rand.New(rand.NewSource(2)).Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repo, st, "patch-limit", Providers{})
	projector.Project(Manifest{
		Version: ManifestVersion, Reason: "bounded binary patch",
		Diff: []DiffRequest{{Question: "binary patch", Paths: []string{"binary.bin"}, BudgetBytes: 1}},
	})
	part := projector.Output().Parts[0]
	if part.Status != PartError || part.Digest != "" ||
		!strings.Contains(part.Reason, "patch execution limit") ||
		!strings.Contains(part.Reason, "binary.bin") {
		t.Fatalf("large tracked patch was accepted or lacked identity: %#v", part)
	}
}

func TestDiffBoundedBinaryRetainsIdentityAndSmallBudgetRefines(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	const path = "bounded.bin"
	if err := os.WriteFile(filepath.Join(repo, path), bytes.Repeat([]byte{0xff, 0}, 64*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	part := NewProjector(repo, st, "bounded-positive", Providers{}).projectDiff(DiffRequest{
		Question: "bounded binary", Paths: []string{path}, BudgetBytes: MaxBudgetBytes,
	})
	if part.Status != PartProjected || part.Digest == "" || part.Diff == nil ||
		len(part.Diff.Files) != 1 || part.Diff.Files[0].WorktreeSHA == "" ||
		!strings.Contains(part.Diff.Body, "base64") {
		t.Fatalf("bounded binary lost content or identity: %#v", part)
	}
	refined := NewProjector(repo, st, "bounded-refinement", Providers{}).projectDiff(DiffRequest{
		Question: "bounded binary", Paths: []string{path}, BudgetBytes: 1,
	})
	if refined.Status != PartRefinement || refined.Digest != part.Digest ||
		refined.Diff == nil || len(refined.Diff.Files) != 1 ||
		refined.Diff.Files[0].WorktreeSHA == "" || refined.Diff.Body != "" {
		t.Fatalf("tiny display budget lost identity or delivered patch: %#v", refined)
	}
}

func TestDiffSingleUntrackedPatchLimit(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	const path = "dense-lines.txt"
	if err := os.WriteFile(filepath.Join(repo, path), bytes.Repeat([]byte{'\n'}, int(maxDiffSourceBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repo, st, "untracked-patch-limit", Providers{})
	projector.Project(Manifest{
		Version: ManifestVersion, Reason: "bounded single untracked patch",
		Diff: []DiffRequest{{Question: "untracked dense lines", Paths: []string{path}, BudgetBytes: 1}},
	})
	part := projector.Output().Parts[0]
	if part.Status != PartError || part.Digest != "" ||
		!strings.Contains(part.Reason, "execution limit") ||
		!strings.Contains(part.Reason, path) {
		t.Fatalf("single untracked patch escaped execution cap: %#v", part)
	}
}

func TestBoundedDiffOutputRejectsOverflowWithoutBufferGrowth(t *testing.T) {
	output := &boundedDiffOutput{limit: 4}
	if n, err := output.Write([]byte("abcd")); n != 4 || err != nil {
		t.Fatalf("exact-limit write: n=%d err=%v", n, err)
	}
	if n, err := output.Write([]byte("excess")); n != 0 || err == nil || !output.exceeded || output.Len() != 4 {
		t.Fatalf("overflow write: n=%d err=%v exceeded=%v bytes=%d", n, err, output.exceeded, output.Len())
	}
}
