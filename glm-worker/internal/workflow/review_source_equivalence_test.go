package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const reviewEquivalenceOldPlan = "# project\n\n## ACTIVE\n\n- [`IMPLEMENTATION_TASKS/active.md`](IMPLEMENTATION_TASKS/active.md)\n\n## NEXT（優先順）\n\n- [`IMPLEMENTATION_TASKS/old-next.md`](IMPLEMENTATION_TASKS/old-next.md)\n\n## BLOCKED\n\n"

func reviewEquivalenceGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}

func reviewEquivalenceWrite(t *testing.T, root, path, contents string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func reviewEquivalenceFixture(t *testing.T) (string, state.SnapshotDigest) {
	t.Helper()
	root := t.TempDir()
	reviewEquivalenceGit(t, root, "init", "-q")
	reviewEquivalenceGit(t, root, "config", "user.name", "Equivalence Fixture")
	reviewEquivalenceGit(t, root, "config", "user.email", "fixture@example.invalid")
	reviewEquivalenceWrite(t, root, state.ParentRulesFile, "# rules\ncontract unchanged\n")
	reviewEquivalenceWrite(t, root, state.ParentPlanFile, reviewEquivalenceOldPlan)
	reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/active.md", "# ACTIVE\nReview source and tests\n")
	reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/old-next.md", "# NEXT\n")
	reviewEquivalenceWrite(t, root, "review.go", "package sample\nfunc ReviewTarget() {}\n")
	reviewEquivalenceWrite(t, root, "review_test.go", "package sample\nfunc TestReviewTarget() {}\n")
	reviewEquivalenceWrite(t, root, "dependency.go", "package sample\nfunc Dependency() {}\n")
	reviewEquivalenceGit(t, root, "add", ".")
	reviewEquivalenceGit(t, root, "commit", "-qm", "baseline")
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func reviewEquivalenceCommitNext(t *testing.T, root string) {
	t.Helper()
	reviewEquivalenceWrite(t, root, state.ParentPlanFile, strings.Replace(reviewEquivalenceOldPlan, "## BLOCKED", "- [`IMPLEMENTATION_TASKS/new-next.md`](IMPLEMENTATION_TASKS/new-next.md)\n\n## BLOCKED", 1))
	reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/new-next.md", "# NEXT\nNot referenced by ACTIVE\n")
	reviewEquivalenceGit(t, root, "add", ".")
	reviewEquivalenceGit(t, root, "commit", "-qm", "metadata-only NEXT fast-forward")
}

func TestEquivalentReviewSourceNextOnlyFastForward(t *testing.T) {
	root, old := reviewEquivalenceFixture(t)
	reviewEquivalenceCommitNext(t, root)
	newSource, err := equivalentReviewSource(root, old, []string{"review.go:1", "review_test.go:1"})
	if err != nil {
		t.Fatal(err)
	}
	if newSource.Head == old.Head || newSource.IndexDigest == old.IndexDigest || newSource.WorktreeDigest != old.WorktreeDigest {
		t.Fatalf("source equivalence lost exact revised HEAD and index: before=%#v after=%#v", old, newSource)
	}
}

func TestEquivalentReviewSourceRejectsPremiseChanges(t *testing.T) {
	tests := []struct {
		name string
		path string
		content string
	}{
		{"ACTIVE task", "IMPLEMENTATION_TASKS/active.md", "# altered ACTIVE\n"},
		{"Rules", state.ParentRulesFile, "# altered rules\n"},
		{"Review code", "review.go", "package sample\nfunc Changed() {}\n"},
		{"Dependent test", "review_test.go", "package sample\nfunc TestChanged() {}\n"},
		{"Dependency source", "dependency.go", "package sample\nfunc Changed() {}\n"},
		{"Existing NEXT Task", "IMPLEMENTATION_TASKS/old-next.md", "# altered\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, old := reviewEquivalenceFixture(t)
			reviewEquivalenceCommitNext(t, root)
			reviewEquivalenceWrite(t, root, tc.path, tc.content)
			reviewEquivalenceGit(t, root, "add", ".")
			reviewEquivalenceGit(t, root, "commit", "-qm", "changed review premise")
			if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
				t.Fatal("changed review premise was reused")
			}
		})
	}
}

func TestEquivalentReviewSourceRejectsChangedPlanAndUnprovableIndex(t *testing.T) {
	t.Run("ACTIVE plan changed", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		reviewEquivalenceWrite(t, root, state.ParentPlanFile, strings.Replace(reviewEquivalenceOldPlan, "## ACTIVE", "## ACTIVE\n\n# changed", 1))
		reviewEquivalenceGit(t, root, "add", ".")
		reviewEquivalenceGit(t, root, "commit", "-qm", "other plan change")
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("changed ACTIVE plan accepted")
		}
	})
	t.Run("unattributed staged file", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		reviewEquivalenceWrite(t, root, "staged-only.go", "package sample\n")
		reviewEquivalenceGit(t, root, "add", "staged-only.go")
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("unattributed staged source accepted")
		}
	})
	t.Run("unknown review target", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		if _, err := equivalentReviewSource(root, old, []string{"missing.go:1"}); err == nil {
			t.Fatal("unaddressable target accepted")
		}
	})
	t.Run("new Task enters ACTIVE reference closure", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/active.md", "# ACTIVE\nref IMPLEMENTATION_TASKS/new-next.md\n")
		reviewEquivalenceGit(t, root, "add", ".")
		reviewEquivalenceGit(t, root, "commit", "-qm", "unresolved reference")
		s, err := state.CaptureGitSnapshot(root)
		if err != nil {
			t.Fatal(err)
		}
		old = state.SnapshotDigest{Head: s.Head, IndexDigest: s.IndexDigest, WorktreeDigest: s.WorktreeDigest}
		reviewEquivalenceCommitNext(t, root)
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("new Task changes old ACTIVE reference closure")
		}
	})
}
