package app

import "testing"

func TestParentReviewDiffCoversQuotedPath(t *testing.T) {
	diff := parentEvidenceDiffBody{
		Body: "diff --git \"a/q\\\"x.go\" \"b/q\\\"x.go\"\n--- \"a/q\\\"x.go\"\n+++ \"b/q\\\"x.go\"\n@@ -1 +1 @@\n-old\n+new\n",
		Files: []parentEvidenceDiffFile{{Path: "q\"x.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
	}
	if !parentReviewDiffCoversTarget("q\"x.go:1", diff) {
		t.Fatal("quoted diff path did not cover matching line target")
	}
}

func TestParentReviewDiffCoversGitOctalQuotedPath(t *testing.T) {
	diff := parentEvidenceDiffBody{
		Body: "diff --git \"a/\\346\\227\\245\\346\\234\\254\\350\\252\\236.go\" \"b/\\346\\227\\245\\346\\234\\254\\350\\252\\236.go\"\n--- \"a/\\346\\227\\245\\346\\234\\254\\350\\252\\236.go\"\n+++ \"b/\\346\\227\\245\\346\\234\\254\\350\\252\\236.go\"\n@@ -1 +1 @@\n-old\n+new\n",
		Files: []parentEvidenceDiffFile{{Path: "日本語.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
	}
	if !parentReviewDiffCoversTarget("日本語.go:1", diff) {
		t.Fatal("octal-quoted diff path did not cover matching line target")
	}
}

func TestParentReviewDiffCoversUnquotedPathWithSpaces(t *testing.T) {
	diff := parentEvidenceDiffBody{
		Body: "diff --git a/hello world.go b/hello world.go\n--- a/hello world.go\n+++ b/hello world.go\n@@ -1 +1 @@\n-old\n+new\n",
		Files: []parentEvidenceDiffFile{{Path: "hello world.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
	}
	if !parentReviewDiffCoversTarget("hello world.go:1", diff) {
		t.Fatal("unquoted diff path with spaces did not cover matching line target")
	}
}

func TestParentReviewDiffBarePathRequiresVisibleSection(t *testing.T) {
	diff := parentEvidenceDiffBody{
		Body: "diff --git a/other.go b/other.go\n--- a/other.go\n+++ b/other.go\n@@ -1 +1 @@\n-old\n+new\n",
		Files: []parentEvidenceDiffFile{{Path: "symbol.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
	}
	if parentReviewDiffCoversTarget("symbol.go", diff) {
		t.Fatal("bare path without a visible diff section counted as proof")
	}
}

func TestParentReviewDiffRejectsUnsupportedTargetSuffix(t *testing.T) {
	diff := parentEvidenceDiffBody{
		Body: "diff --git a/symbol.go b/symbol.go\n--- a/symbol.go\n+++ b/symbol.go\n@@ -1 +1 @@ TargetSymbol\n-old\n+new\n",
		Files: []parentEvidenceDiffFile{{Path: "symbol.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
	}
	for _, target := range []string{"symbol.go OtherSymbol", "symbol.go,OtherSymbol"} {
		if parentReviewDiffCoversTarget(target, diff) {
			t.Fatalf("unsupported target suffix counted as proof: %q", target)
		}
	}
}
