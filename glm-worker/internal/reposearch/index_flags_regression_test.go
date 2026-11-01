package reposearch

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepoSearchRejectsAssumeUnchangedInsteadOfServingStaleCache(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "data.txt"), "oldneedle\n")
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir()}
	first, err := Search(context.Background(), dir, "oldneedle", opts)
	if err != nil || first.CacheStatus != CacheStatusRebuilt {
		t.Fatalf("initial search failed: %#v %v", first, err)
	}
	hit, err := Search(context.Background(), dir, "oldneedle", opts)
	if err != nil || hit.CacheStatus != CacheStatusHit {
		t.Fatalf("ordinary cache hit regressed: %#v %v", hit, err)
	}
	gitRun(t, dir, "update-index", "--assume-unchanged", "data.txt")
	writeTestFile(t, filepath.Join(dir, "data.txt"), "newneedle\n")
	for _, options := range []Options{opts, {DisableCache: true}} {
		if report, err := Search(context.Background(), dir, "newneedle", options); !errors.Is(err, ErrUnsupportedIndexFlags) {
			t.Fatalf("unsupported index returned stale/success result: %#v %v", report, err)
		}
	}
	if report, err := ExhaustiveSearch(context.Background(), dir, "newneedle", ExhaustiveOptions{}); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("exhaustive search ignored unsupported index: %#v %v", report, err)
	}
	gitRun(t, dir, "update-index", "--no-assume-unchanged", "data.txt")
	fresh, err := Search(context.Background(), dir, "newneedle", opts)
	if err != nil || fresh.CacheStatus != CacheStatusRebuilt ||
		!reflect.DeepEqual(resultPaths(fresh), []string{"data.txt"}) {
		t.Fatalf("unflagged content not rebuilt: %#v %v", fresh, err)
	}
}

func TestRepoSearchRejectsSkipWorktreeBeforeCacheOrExhaustiveSearch(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "data.txt"), "needle\n")
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir()}
	if _, err := Search(context.Background(), dir, "needle", opts); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "update-index", "--skip-worktree", "data.txt")
	for _, options := range []Options{opts, {DisableCache: true}} {
		if _, err := Search(context.Background(), dir, "needle", options); !errors.Is(err, ErrUnsupportedIndexFlags) {
			t.Fatalf("skip-worktree accepted: %v", err)
		}
	}
	if _, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{}); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("exhaustive skip-worktree accepted: %v", err)
	}
	gitRun(t, dir, "update-index", "--no-skip-worktree", "data.txt")
	if report, err := Search(context.Background(), dir, "needle", opts); err != nil || report.CacheStatus != CacheStatusHit {
		t.Fatalf("unflagged normal cache hit regressed: %#v %v", report, err)
	}
}

func TestRepoSearchRejectsSparseCheckoutOutsideMaterializedCorpus(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "keep", "visible.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "hidden", "invisible.txt"), "needle\n")
	commitAll(t, dir, "initial")
	gitRun(t, dir, "sparse-checkout", "init", "--cone")
	gitRun(t, dir, "sparse-checkout", "set", "keep")
	if _, err := Search(context.Background(), dir, "needle", Options{DisableCache: true}); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("sparse checkout was not rejected: %v", err)
	}
}
