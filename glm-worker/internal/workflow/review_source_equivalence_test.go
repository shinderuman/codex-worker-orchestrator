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

func TestEquivalentReviewSourceRejectsSameHead(t *testing.T) {
	root, original := reviewEquivalenceFixture(t)
	current, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if original != (state.SnapshotDigest{Head: current.Head, IndexDigest: current.IndexDigest, WorktreeDigest: current.WorktreeDigest}) {
		t.Fatal("same-HEAD fixture did not preserve the accepted HEAD, index and worktree")
	}
	if _, err := equivalentReviewSource(root, original, []string{"review.go:1"}); err == nil ||
		!strings.Contains(err.Error(), "no committed advancement") {
		t.Fatalf("unchanged source must take ordinary review reentry, not equivalence: %v", err)
	}
}

func TestEquivalentReviewSourceRejectsPremiseChanges(t *testing.T) {
	tests := []struct {
		name    string
		path    string
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

func reviewEquivalenceReferenceFixture(t *testing.T, path, content string) (string, state.SnapshotDigest) {
	t.Helper()
	root, _ := reviewEquivalenceFixture(t)
	reviewEquivalenceWrite(t, root, path, content)
	reviewEquivalenceGit(t, root, "add", ".")
	reviewEquivalenceGit(t, root, "commit", "-qm", "preexisting unresolved metadata reference")
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
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
	t.Run("unattributed staged Plan edit", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		planPath := filepath.Join(root, state.ParentPlanFile)
		plan, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		reviewEquivalenceWrite(t, root, state.ParentPlanFile, string(plan)+"\nchanged staged Plan\n")
		reviewEquivalenceGit(t, root, "add", state.ParentPlanFile)
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("staged metadata changed beyond the committed fast-forward")
		}
	})
	t.Run("unattributed staged added Task edit", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/new-next.md", "# different staged Task\n")
		reviewEquivalenceGit(t, root, "add", "IMPLEMENTATION_TASKS/new-next.md")
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("staged new Task does not match the committed advancement")
		}
	})
	t.Run("unknown review target", func(t *testing.T) {
		root, old := reviewEquivalenceFixture(t)
		reviewEquivalenceCommitNext(t, root)
		if _, err := equivalentReviewSource(root, old, []string{"missing.go:1"}); err == nil {
			t.Fatal("unaddressable target accepted")
		}
	})
	t.Run("new Task is referenced by an existing NEXT Task", func(t *testing.T) {
		root, old := reviewEquivalenceReferenceFixture(t, "IMPLEMENTATION_TASKS/old-next.md", "# NEXT\\nDepends on IMPLEMENTATION_TASKS/new-next.md\\n")
		reviewEquivalenceCommitNext(t, root)
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("new Task resolved a pre-existing transitive reference")
		}
	})
	t.Run("new Task enters ACTIVE reference closure", func(t *testing.T) {
		root, old := reviewEquivalenceReferenceFixture(t, "IMPLEMENTATION_TASKS/active.md", "# ACTIVE\\nref IMPLEMENTATION_TASKS/new-next.md\\n")
		reviewEquivalenceCommitNext(t, root)
		if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
			t.Fatal("new Task changes old ACTIVE reference closure")
		}
	})
}

func reviewEquivalenceCanonicalPlanFixture(t *testing.T, activeRow, nextRow string) (string, state.SnapshotDigest, string) {
	t.Helper()
	root, _ := reviewEquivalenceFixture(t)
	plan := strings.Replace(reviewEquivalenceOldPlan,
		"- [\x60IMPLEMENTATION_TASKS/active.md\x60](IMPLEMENTATION_TASKS/active.md)", activeRow, 1)
	plan = strings.Replace(plan,
		"- [\x60IMPLEMENTATION_TASKS/old-next.md\x60](IMPLEMENTATION_TASKS/old-next.md)", nextRow, 1)
	if plan != reviewEquivalenceOldPlan {
		reviewEquivalenceWrite(t, root, state.ParentPlanFile, plan)
		reviewEquivalenceGit(t, root, "add", state.ParentPlanFile)
		reviewEquivalenceGit(t, root, "commit", "-qm", "canonical entry baseline")
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, state.SnapshotDigest{
		Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest,
	}, plan
}

func TestEquivalentReviewSourceCanonicalTaskEntryForms(t *testing.T) {
	forms := []struct {
		name string
		row  func(string) string
	}{
		{"plain", func(path string) string { return "- " + path }},
		{"backtick", func(path string) string { return "- \x60" + path + "\x60" }},
		{"link", func(path string) string { return "- [\x60" + path + "\x60](" + path + ")" }},
	}
	for activeIndex, active := range forms {
		for nextIndex, next := range forms {
			t.Run(active.name+" ACTIVE / "+next.name+" NEXT", func(t *testing.T) {
				root, old, plan := reviewEquivalenceCanonicalPlanFixture(t,
					active.row("IMPLEMENTATION_TASKS/active.md"),
					next.row("IMPLEMENTATION_TASKS/old-next.md"))
				newRow := forms[(activeIndex+nextIndex+1)%len(forms)].row("IMPLEMENTATION_TASKS/new-next.md")
				reviewEquivalenceWrite(t, root, state.ParentPlanFile,
					strings.Replace(plan, "## BLOCKED", newRow+"\n\n## BLOCKED", 1))
				reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/new-next.md", "# new NEXT\n")
				reviewEquivalenceGit(t, root, "add", ".")
				reviewEquivalenceGit(t, root, "commit", "-qm", "canonical NEXT-only fast-forward")
				current, err := equivalentReviewSource(root, old, []string{"review.go:1", "review_test.go:1"})
				if err != nil {
					t.Fatal(err)
				}
				if current.Head == old.Head || current.IndexDigest == old.IndexDigest ||
					current.WorktreeDigest != old.WorktreeDigest {
					t.Fatalf("fast-forward identity changed: old=%#v new=%#v", old, current)
				}
			})
		}
	}
}

func TestEquivalentReviewSourcePlainEntriesPreserveNonNextRejection(t *testing.T) {
	cases := []struct {
		name   string
		change func(string) string
	}{
		{"ACTIVE representation changed", func(plan string) string {
			return strings.Replace(plan, "- IMPLEMENTATION_TASKS/active.md",
				"- \x60IMPLEMENTATION_TASKS/active.md\x60", 1)
		}},
		{"BLOCKED changed", func(plan string) string {
			return strings.Replace(plan, "## BLOCKED\n\n", "## BLOCKED\n\n- IMPLEMENTATION_TASKS/blocked.md\n", 1)
		}},
		{"GOAL changed", func(plan string) string {
			return strings.Replace(plan, "# project\n", "# project\n\n## GOAL\n\n- different goal\n", 1)
		}},
		{"invalid NEXT link", func(plan string) string {
			return strings.Replace(plan, "- IMPLEMENTATION_TASKS/new-next.md",
				"- [\x60IMPLEMENTATION_TASKS/new-next.md\x60](IMPLEMENTATION_TASKS/other.md)", 1)
		}},
		{"invalid NEXT path", func(plan string) string {
			return strings.Replace(plan, "- IMPLEMENTATION_TASKS/new-next.md",
				"- IMPLEMENTATION_TASKS/../new-next.md", 1)
		}},
		{"duplicate NEXT heading", func(plan string) string {
			return strings.Replace(plan, "## BLOCKED", "## NEXT\n\n## BLOCKED", 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, old, base := reviewEquivalenceCanonicalPlanFixture(t,
				"- IMPLEMENTATION_TASKS/active.md", "- IMPLEMENTATION_TASKS/old-next.md")
			next := strings.Replace(base, "## BLOCKED", "- IMPLEMENTATION_TASKS/new-next.md\n\n## BLOCKED", 1)
			reviewEquivalenceWrite(t, root, state.ParentPlanFile, tc.change(next))
			reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/new-next.md", "# new NEXT\n")
			reviewEquivalenceGit(t, root, "add", ".")
			reviewEquivalenceGit(t, root, "commit", "-qm", "non-NEXT or invalid change")
			if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
				t.Fatal("non-NEXT change or invalid NEXT entry was accepted")
			}
		})
	}
}

func TestEquivalentReviewSourcePlainEntriesRejectReorderedNext(t *testing.T) {
	root, _, plan := reviewEquivalenceCanonicalPlanFixture(t,
		"- IMPLEMENTATION_TASKS/active.md", "- IMPLEMENTATION_TASKS/old-next.md")
	originalNext := "- IMPLEMENTATION_TASKS/old-next.md\n"
	secondNext := "- \x60IMPLEMENTATION_TASKS/second-next.md\x60\n"
	plan = strings.Replace(plan, originalNext, originalNext+secondNext, 1)
	reviewEquivalenceWrite(t, root, state.ParentPlanFile, plan)
	reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/second-next.md", "# second NEXT\n")
	reviewEquivalenceGit(t, root, "add", ".")
	reviewEquivalenceGit(t, root, "commit", "-qm", "two original NEXT tasks")
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	old := state.SnapshotDigest{
		Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest,
	}
	reordered := strings.Replace(plan, originalNext+secondNext,
		secondNext+originalNext+"- IMPLEMENTATION_TASKS/new-next.md\n", 1)
	reviewEquivalenceWrite(t, root, state.ParentPlanFile, reordered)
	reviewEquivalenceWrite(t, root, "IMPLEMENTATION_TASKS/new-next.md", "# new NEXT\n")
	reviewEquivalenceGit(t, root, "add", ".")
	reviewEquivalenceGit(t, root, "commit", "-qm", "reordered original NEXT tasks")
	if _, err := equivalentReviewSource(root, old, []string{"review.go:1"}); err == nil {
		t.Fatal("reordered NEXT tasks passed review-source equivalence")
	}
}

func TestReviewPlanActivePathRejectsNoncanonicalOrDuplicateHeadings(t *testing.T) {
	const row = "- IMPLEMENTATION_TASKS/active.md\n"
	for _, plan := range []string{
		"## ACTIVE description\n" + row,
		"## ACTIVE\n" + row + "## ACTIVE\n",
	} {
		if path, ok := reviewPlanActivePath(plan); ok || path != "" {
			t.Fatalf("ambiguous ACTIVE authority identified: %q from %q", path, plan)
		}
	}
}
