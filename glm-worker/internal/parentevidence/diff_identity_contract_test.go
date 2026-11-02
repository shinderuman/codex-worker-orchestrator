package parentevidence

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func captureIdentityPart(t *testing.T, repoRoot string, paths ...string) Part {
	t.Helper()
	projector := NewProjector(repoRoot, nil, "identity-test", Providers{})
	part := projector.projectDiff(DiffRequest{Question: "review changed evidence", Paths: paths, BudgetBytes: MaxBudgetBytes})
	if part.Status != PartProjected || part.Diff == nil || part.Digest == "" {
		t.Fatalf("diff projection = %#v", part)
	}
	return part
}

func assertChangedDiffIdentity(t *testing.T, before, after Part) {
	t.Helper()
	if before.Digest == after.Digest {
		t.Fatalf("different diff evidence reused digest %s: before=%#v after=%#v", before.Digest, before.Diff, after.Diff)
	}
}

func TestDiffIdentityModeOnlyChangeAndStableReplay(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	gitReviewCoverageCommand(t, repoRoot, "config", "core.filemode", "true")
	before := captureIdentityPart(t, repoRoot, "review.go")
	if err := os.Chmod(filepath.Join(repoRoot, "review.go"), 0o700); err != nil {
		t.Fatal(err)
	}
	after := captureIdentityPart(t, repoRoot, "review.go")
	if before.Diff.Body == after.Diff.Body || before.Diff.Files[0].WorktreeMode == after.Diff.Files[0].WorktreeMode {
		t.Fatalf("mode-only change was not reflected in delivered evidence: before=%#v after=%#v", before.Diff, after.Diff)
	}
	assertChangedDiffIdentity(t, before, after)
	if replay := captureIdentityPart(t, repoRoot, "review.go"); replay.Digest != after.Digest {
		t.Fatalf("identical reread changed digest: %s != %s", replay.Digest, after.Digest)
	}
}

func TestDiffIdentityDirectoryChildPatchChange(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	dir := filepath.Join(repoRoot, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "child.txt")
	if err := os.WriteFile(child, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repoRoot, "add", "dir/child.txt")
	gitReviewCoverageCommand(t, repoRoot, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-qm", "add child")
	before := captureIdentityPart(t, repoRoot, "dir")
	if err := os.WriteFile(child, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := captureIdentityPart(t, repoRoot, "dir")
	if before.Diff.Body == after.Diff.Body {
		t.Fatal("directory child change failed to update patch")
	}
	assertChangedDiffIdentity(t, before, after)
}

func TestDiffIdentityUntrackedContentAndFileType(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	path := filepath.Join(repoRoot, "new.txt")
	if err := os.WriteFile(path, []byte("review.go"), 0o644); err != nil {
		t.Fatal(err)
	}
	regular := captureIdentityPart(t, repoRoot, "new.txt")
	if err := os.WriteFile(path, []byte("changed content"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := captureIdentityPart(t, repoRoot, "new.txt")
	assertChangedDiffIdentity(t, regular, changed)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("review.go", path); err != nil {
		t.Fatal(err)
	}
	link := captureIdentityPart(t, repoRoot, "new.txt")
	// The original regular file and the symlink carry identical content bytes.
	if regular.Diff.Files[0].WorktreeSHA != link.Diff.Files[0].WorktreeSHA {
		t.Fatalf("fixture bytes differ unexpectedly: regular=%#v link=%#v", regular.Diff, link.Diff)
	}
	if regular.Diff.Files[0].WorktreeMode == link.Diff.Files[0].WorktreeMode {
		t.Fatal("regular file and symlink have identical mode identity")
	}
	assertChangedDiffIdentity(t, regular, link)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("another-target", path); err != nil {
		t.Fatal(err)
	}
	assertChangedDiffIdentity(t, link, captureIdentityPart(t, repoRoot, "new.txt"))
}

func TestDiffIdentityTrackedDeleteAndRename(t *testing.T) {
	for _, mode := range []string{"delete", "rename"} {
		t.Run(mode, func(t *testing.T) {
			repoRoot, _ := newReviewCoverageStore(t)
			before := captureIdentityPart(t, repoRoot, "review.go", "moved.go")
			src := filepath.Join(repoRoot, "review.go")
			if mode == "delete" {
				if err := os.Remove(src); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(src, filepath.Join(repoRoot, "moved.go")); err != nil {
				t.Fatal(err)
			}
			after := captureIdentityPart(t, repoRoot, "review.go", "moved.go")
			if after.Diff.Body == before.Diff.Body {
				t.Fatalf("%s did not alter delivered patch", mode)
			}
			assertChangedDiffIdentity(t, before, after)
		})
	}
}

func TestDiffIdentityDeliveryAndReviewProofBinding(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openLocatorIdentityReview(t, repoRoot, st, []string{"review.go:" + reviewtarget.WholeFileDiffLocator})
	deliver := func() Part {
		t.Helper()
		projector := NewProjector(repoRoot, st, "diff-delivery", Providers{})
		projector.Project(Manifest{
			Version: ManifestVersion,
			Reason: "review diff delivery",
			Diff: []DiffRequest{{Question: "review", Paths: []string{"review.go"}, BudgetBytes: MaxBudgetBytes}},
		})
		var output bytes.Buffer
		if err := projector.Commit(&output, "diff identity test"); err != nil {
			t.Fatal(err)
		}
		return projector.Output().Parts[0]
	}
	first := deliver()
	if first.Diff == nil || first.Diff.Body == "" || first.Status != PartProjected {
		t.Fatalf("initial diff body not delivered: %#v", first)
	}
	_, recorded, err := st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceDiff, first.Digest)
	if err != nil || !recorded {
		t.Fatalf("diff digest not recorded in ledger: recorded=%v err=%v", recorded, err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof == nil || len(binding.Proof.Claims) != 1 || binding.Proof.Claims[0].Digest != first.Digest {
		t.Fatalf("review proof is not bound to delivered diff: %#v", binding)
	}
	replayed := deliver()
	if replayed.Digest != first.Digest || replayed.Diff.Body != "" || replayed.Reason != UnchangedReason {
		t.Fatalf("identical evidence was not deduplicated: %#v", replayed)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	next := deliver()
	if next.Diff == nil || next.Diff.Body == "" || next.Reason == UnchangedReason {
		t.Fatalf("changed diff was incorrectly suppressed: %#v", next)
	}
	assertChangedDiffIdentity(t, first, next)
}

func TestDiffIdentityDoesNotFollowIntermediateSymlinkOutsideRepository(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repoRoot, "external")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := diffWorktreeIdentity(repoRoot, "external/secret.txt"); err == nil {
		t.Fatal("diff worktree identity crossed repository boundary")
	}
}
