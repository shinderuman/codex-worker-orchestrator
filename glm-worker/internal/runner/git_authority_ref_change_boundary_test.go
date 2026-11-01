package runner

import "testing"

func TestGitAuthorityRefChangesKeepBothSymbolicHeadTargets(t *testing.T) {
	t.Parallel()
	oldRef := "refs/heads/old"
	newRef := "refs/heads/new"
	previous := gitAuthoritySnapshot{
		symbolicHead: oldRef,
		refs: []GitRefState{
			{Name: oldRef, ObjectID: "old-oid"},
			{Name: "refs/heads/unrelated", ObjectID: "other-oid"},
		},
	}
	next := gitAuthoritySnapshot{
		symbolicHead: newRef,
		refs: []GitRefState{
			{Name: newRef, ObjectID: "new-oid"},
			{Name: "refs/heads/unrelated", ObjectID: "changed-oid"},
		},
	}
	changes, truncated := gitAuthorityRefChanges(previous, next)
	if truncated || len(changes) != 2 {
		t.Fatalf("symbolic HEAD move evidence = %#v, truncated=%t", changes, truncated)
	}
	if changes[0].Name != newRef || changes[0].Before != nil ||
		changes[0].After == nil || changes[0].After.ObjectID != "new-oid" {
		t.Fatalf("new symbolic HEAD target not preserved: %#v", changes[0])
	}
	if changes[1].Name != oldRef || changes[1].After != nil ||
		changes[1].Before == nil || changes[1].Before.ObjectID != "old-oid" {
		t.Fatalf("old symbolic HEAD target not preserved: %#v", changes[1])
	}
}

func TestGitAuthorityRefChangesPreserveDeletionWithoutUnrelatedChurn(t *testing.T) {
	t.Parallel()
	mainRef := "refs/heads/main"
	before := gitAuthoritySnapshot{
		symbolicHead: mainRef,
		refs: []GitRefState{{Name: mainRef, ObjectID: "old-oid"}},
	}
	after := gitAuthoritySnapshot{
		symbolicHead: mainRef,
		refs: []GitRefState{{Name: "refs/heads/unrelated", ObjectID: "unrelated-oid"}},
	}
	changes, truncated := gitAuthorityRefChanges(before, after)
	if truncated || len(changes) != 1 || changes[0].Name != mainRef ||
		changes[0].Before == nil || changes[0].Before.ObjectID != "old-oid" ||
		changes[0].After != nil {
		t.Fatalf("deleted authority ref evidence = %#v, truncated=%t", changes, truncated)
	}
}
