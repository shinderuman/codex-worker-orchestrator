package parentevidence

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestWholeFileDiffLocatorProducesDiffClaim(t *testing.T) {
	diff := DiffBody{
		Paths: []string{"foo.go"},
		Body:  "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new\n",
		Files: []DiffFile{{Path: "foo.go", Status: "M", HeadBlob: "head", WorktreeSHA: "worktree"}},
	}
	claims, ok := ReviewClaims([]string{"foo.go:" + reviewtarget.WholeFileDiffLocator}, []Part{{
		Kind: "diff", Digest: "digest", Locator: "git diff HEAD -- foo.go", Diff: &diff,
	}})
	if !ok || len(claims) != 1 || claims[0].Kind != "diff" {
		t.Fatalf("whole-file diff claims = %#v complete=%v", claims, ok)
	}
}

func TestDiffRemainsOrdinarySymbolLocator(t *testing.T) {
	path, locator, err := ReviewTarget("foo.go:diff")
	if err != nil {
		t.Fatal(err)
	}
	if path != "foo.go" || locator != "diff" {
		t.Fatalf("symbol target parsed as path=%q locator=%q", path, locator)
	}
	if locator == reviewtarget.WholeFileDiffLocator {
		t.Fatalf("ordinary symbol locator %q collided with reserved whole-file locator", locator)
	}

	diff := DiffBody{
		Paths: []string{"foo.go"},
		Body:  "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+func diff() {}\n",
		Files: []DiffFile{{Path: "foo.go", Status: "M", HeadBlob: "head", WorktreeSHA: "worktree"}},
	}
	if !ReviewDiffCoversTarget("foo.go:diff", diff) {
		t.Fatal("ordinary diff symbol locator no longer follows symbol evidence semantics")
	}
}
