package reposearch

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"fmt"
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

func TestRepoSearchIndexFlagsScopedToSearchCorpus(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "visible.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "generated", "hidden.txt"), "needle\n")
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir(), ExcludeDirs: []string{"generated"}}
	if report, err := Search(context.Background(), dir, "needle", opts); err != nil || report.CacheStatus != CacheStatusRebuilt {
		t.Fatalf("initial search: %#v %v", report, err)
	}
	gitRun(t, dir, "update-index", "--assume-unchanged", "generated/hidden.txt")
	writeTestFile(t, filepath.Join(dir, "generated", "hidden.txt"), "changedneedle\n")
	if report, err := Search(context.Background(), dir, "needle", opts); err != nil ||
		report.CacheStatus != CacheStatusHit || !reflect.DeepEqual(resultPaths(report), []string{"visible.txt"}) {
		t.Fatalf("excluded flagged file invalidated/rejected in-scope cache: %#v %v", report, err)
	}
	if report, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{ExcludeDirs: []string{"generated"}}); err != nil ||
		len(report.Matches) != 1 || report.Matches[0].Path != "visible.txt" {
		t.Fatalf("excluded flagged file invalidated exhaustive search: %#v %v", report, err)
	}
	if _, err := Search(context.Background(), dir, "needle", Options{DisableCache: true}); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("unexcluded flagged file silently accepted: %v", err)
	}
}

func TestRepoSearchIndexFlagAppearingDuringSearchFailsClosed(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "data.txt"), "oldneedle\n")
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir()}
	if _, err := Search(context.Background(), dir, "oldneedle", opts); err != nil {
		t.Fatal(err)
	}
	original := captureFingerprint
	calls := 0
	captureFingerprint = func(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (fingerprint, error) {
		calls++
		if calls == 2 {
			gitRun(t, dir, "update-index", "--assume-unchanged", "data.txt")
			writeTestFile(t, filepath.Join(dir, "data.txt"), "newneedle\n")
		}
		return original(ctx, repoRoot, excludeDirs)
	}
	defer func() { captureFingerprint = original }()
	if report, err := Search(context.Background(), dir, "newneedle", opts); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("mid-search flag accepted as stale result: %#v %v", report, err)
	}
	if calls < 2 {
		t.Fatal("search did not reach post-search fingerprint")
	}
}

func TestRepoSearchIndexFlagsCostRepresentativeCorpus(t *testing.T) {
	dir := initRepo(t)
	const files = 128
	for i := 0; i < files; i++ {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("data-%03d.txt", i)), "needle for timing\n")
	}
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir()}
	if _, err := Search(context.Background(), dir, "needle", opts); err != nil {
		t.Fatal(err)
	}
	const iterations = 8
	measureLsFiles := func(flags ...string) time.Duration {
		start := time.Now()
		for i := 0; i < iterations; i++ {
			if _, err := gitOutput(context.Background(), dir, append([]string{"ls-files", "-z"}, flags...)...); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start)
	}
	before := measureLsFiles("-s", "--cached")
	after := measureLsFiles("-v", "-s", "--cached")
	start := time.Now()
	for i := 0; i < iterations; i++ {
		report, err := Search(context.Background(), dir, "needle", opts)
		if err != nil || report.CacheStatus != CacheStatusHit || report.IndexedFiles != files {
			t.Fatalf("cache-hit #%d invalid: %#v %v", i, report, err)
		}
	}
	hits := time.Since(start)
	t.Logf("corpus=%d files, %d iterations: ls-files -s=%s, ls-files -v -s=%s, cached Search=%s total (all use existing subprocess count)", files, iterations, before, after, hits)
}
