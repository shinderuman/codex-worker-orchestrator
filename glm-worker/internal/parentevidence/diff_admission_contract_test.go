package parentevidence

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDiffExactPathAdmission(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.Mkdir(filepath.Join(repoRoot, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ".", "..", "../escape", "/absolute", "dir/", "*.go", "review?.go", "a[bc].go", ":(glob)review.go", "review\x00.go", "a/../review.go"} {
		t.Run(strings.ReplaceAll(path, "\x00", "NUL"), func(t *testing.T) {
			manifest := identityAdmissionManifest(path)
			if err := ValidateManifest(manifest); err == nil {
				t.Fatalf("invalid exact path %q was accepted", path)
			}
		})
	}
	for _, path := range []string{"dir", "missing.go"} {
		t.Run(path, func(t *testing.T) {
			manifest := identityAdmissionManifest(path)
			if err := ValidateManifest(manifest); err != nil {
				t.Fatal(err)
			}
			projector := NewProjector(repoRoot, st, "rejected-path", Providers{})
			projector.Project(manifest)
			part := projector.Output().Parts[0]
			if part.Status != PartError || part.Digest != "" {
				t.Fatalf("invalid path was projected: %#v", part)
			}
			if err := projector.Commit(&bytes.Buffer{}, "reject invalid path"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func identityAdmissionManifest(paths ...string) Manifest {
	return Manifest{
		Version: ManifestVersion,
		Reason:  "exact diff path admission",
		Diff: []DiffRequest{{
			Question: "exact diff",
			Paths: paths,
			BudgetBytes: MaxBudgetBytes,
		}},
	}
}

func TestDiffDeletedDirectoryAndIntermediateSymlinkRejected(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	dir := filepath.Join(repoRoot, "tracked-dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repoRoot, "add", "tracked-dir/child.txt")
	gitReviewCoverageCommand(t, repoRoot, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-qm", "track directory child")
	if err := os.Remove(filepath.Join(dir, "child.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"tracked-dir", "tracked-dir/child.txt"} {
		projector := NewProjector(repoRoot, st, "deleted-directory", Providers{})
		projector.Project(identityAdmissionManifest(path))
		part := projector.Output().Parts[0]
		if path == "tracked-dir" && part.Status != PartError {
			t.Fatalf("deleted directory was accepted: %#v", part)
		}
		if path == "tracked-dir/child.txt" && part.Status != PartProjected {
			t.Fatalf("deleted tracked exact child was rejected: %#v", part)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repoRoot, "alias")); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, "symlink-parent", Providers{})
	projector.Project(identityAdmissionManifest("alias/outside.txt"))
	if part := projector.Output().Parts[0]; part.Status != PartError {
		t.Fatalf("intermediate symlink was accepted: %#v", part)
	}
}

func TestDiffIndexOnlyAndLiteralNames(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	for _, path := range []string{"space and \"quote\".txt", "-leading.txt"} {
		if err := os.WriteFile(filepath.Join(repoRoot, path), []byte("before\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := captureIdentityPart(t, repoRoot, path)
		if err := os.WriteFile(filepath.Join(repoRoot, path), []byte("after\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		after := captureIdentityPart(t, repoRoot, path)
		assertChangedDiffIdentity(t, before, after)
		if !strings.Contains(after.Diff.Body, "after") {
			t.Fatalf("literal untracked name %q lost body", path)
		}
	}
	path := filepath.Join(repoRoot, "review.go")
	if err := os.WriteFile(path, []byte("package review\nvar target = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stagedBefore := captureIdentityPart(t, repoRoot, "review.go")
	gitReviewCoverageCommand(t, repoRoot, "add", "--", "review.go")
	stagedAfter := captureIdentityPart(t, repoRoot, "review.go")
	if stagedAfter.Diff.Files[0].IndexBlob == stagedBefore.Diff.Files[0].IndexBlob {
		t.Fatal("index-only change had no updated index blob")
	}
	assertChangedDiffIdentity(t, stagedBefore, stagedAfter)
}

func TestDiffLeafSymlinkAndUnknownType(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	external := t.TempDir()
	path := filepath.Join(repoRoot, "link")
	if err := os.Symlink(filepath.Join(external, "missing.txt"), path); err != nil {
		t.Fatal(err)
	}
	link := captureIdentityPart(t, repoRoot, "link")
	if link.Diff.Files[0].WorktreeSHA == "" || !strings.Contains(link.Diff.Body, "missing.txt") {
		t.Fatalf("outside dangling leaf link was not delivered: %#v", link)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "different.txt"), path); err != nil {
		t.Fatal(err)
	}
	assertChangedDiffIdentity(t, link, captureIdentityPart(t, repoRoot, "link"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, _, _, _, err := diffWorktreeObject(root, "link"); err == nil {
		t.Fatal("nonregular worktree object had a successful identity")
	}
}

func TestDiffSnapshotChangeBeforeCommitFailsClosed(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, "changed-snapshot", Providers{})
	projector.Project(identityAdmissionManifest("review.go"))
	original := projector.Output().Parts[0]
	if original.Status != PartProjected {
		t.Fatalf("initial evidence was not projected: %#v", original)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := projector.Commit(&bytes.Buffer{}, "must reject source change"); err == nil {
		t.Fatal("stale diff evidence was committed")
	}
	_, delivered, err := st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceDiff, original.Digest)
	if err != nil || delivered {
		t.Fatalf("undelivered evidence marked delivered: %v %v", delivered, err)
	}
}

func TestDiffRefinementBudgetDoesNotRecordDelivery(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, "too-small-budget", Providers{})
	projector.Project(Manifest{Version: ManifestVersion, Reason: "tiny diff budget",
		Diff: []DiffRequest{{Question: "review diff", Paths: []string{"review.go"}, BudgetBytes: 1}}})
	part := projector.Output().Parts[0]
	if part.Status != PartRefinement || part.Diff == nil || part.Diff.Body != "" {
		t.Fatalf("small-budget diff evidence was delivered: %#v", part)
	}
	if err := projector.Commit(&bytes.Buffer{}, "diff refinement"); err != nil {
		t.Fatal(err)
	}
	_, delivered, err := st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceDiff, part.Digest)
	if err != nil || delivered {
		t.Fatalf("refined body was recorded as delivered: %v %v", delivered, err)
	}
}

func TestDiffTotalBudgetDropsUndeliveredClaim(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	paths := []string{"big-a.txt", "big-b.txt"}
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(repoRoot, path), []byte(strings.Repeat("a", 54*1024)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	projector := NewProjector(repoRoot, st, "total-budget", Providers{})
	projector.Project(Manifest{Version: ManifestVersion, Reason: "large diff payloads",
		Diff: []DiffRequest{
			{Question: "a", Paths: []string{paths[0]}, BudgetBytes: MaxBudgetBytes},
			{Question: "b", Paths: []string{paths[1]}, BudgetBytes: MaxBudgetBytes},
		}})
	if projector.Output().Parts[0].Status != PartProjected || projector.Output().Parts[1].Status != PartProjected {
		t.Fatal("fixture diff projections unexpectedly refined")
	}
	var out bytes.Buffer
	if err := projector.Commit(&out, "total budget"); err != nil {
		t.Fatal(err)
	}
	parts := projector.Output().Parts
	if parts[0].Diff == nil || parts[0].Diff.Body == "" ||
		parts[1].Diff == nil || parts[1].Diff.Body != "" || parts[1].Status != PartRefinement {
		t.Fatalf("total budget did not strip only one diff body: %#v", parts)
	}
	for i, part := range parts {
		_, delivered, err := st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceDiff, part.Digest)
		if err != nil || delivered != (i == 0) {
			t.Fatalf("unexpected delivery for part %d: %v %v", i, delivered, err)
		}
	}
}
