package reposearch

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

func TestRepoSearchSparseCheckoutAndSparseIndexRespectCorpusBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		flag string
	}{
		{name: "normal index", flag: "--no-sparse-index"},
		{name: "sparse index", flag: "--sparse-index"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := initRepo(t)
			writeTestFile(t, filepath.Join(dir, "keep", "visible.txt"), "needle\n")
			writeTestFile(t, filepath.Join(dir, "hidden", "invisible.txt"), "needle\n")
			writeTestFile(t, filepath.Join(dir, "root.txt"), "unrelated\n")
			commitAll(t, dir, "initial")
			gitRun(t, dir, "sparse-checkout", "init", "--cone")
			gitRun(t, dir, "sparse-checkout", "set", test.flag, "keep")
			output, err := gitOutput(context.Background(), dir, "ls-files", "-z", "-v", "-s", "--cached")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "\thidden/invisible.txt\x00") {
				t.Fatalf("sparse tracked entry was not expanded by ls-files: %q", output)
			}
			if _, err := Search(context.Background(), dir, "needle", Options{DisableCache: true}); !errors.Is(err, ErrUnsupportedIndexFlags) {
				t.Fatalf("in-corpus sparse entry accepted: %v", err)
			}
			if _, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{}); !errors.Is(err, ErrUnsupportedIndexFlags) {
				t.Fatalf("in-corpus sparse entry accepted as exhaustive: %v", err)
			}
			opts := Options{CacheRoot: t.TempDir(), ExcludeDirs: []string{"hidden"}}
			first, err := Search(context.Background(), dir, "needle", opts)
			if err != nil || first.CacheStatus != CacheStatusRebuilt ||
				!reflect.DeepEqual(resultPaths(first), []string{"keep/visible.txt"}) {
				t.Fatalf("excluded sparse directory blocked scoped corpus: %#v %v", first, err)
			}
			hit, err := Search(context.Background(), dir, "needle", opts)
			if err != nil || hit.CacheStatus != CacheStatusHit ||
				!reflect.DeepEqual(resultPaths(hit), []string{"keep/visible.txt"}) {
				t.Fatalf("excluded sparse directory broke cache hit: %#v %v", hit, err)
			}
			exhaustive, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{ExcludeDirs: []string{"hidden"}})
			if err != nil || len(exhaustive.Matches) != 1 || exhaustive.Matches[0].Path != "keep/visible.txt" {
				t.Fatalf("excluded sparse directory broke exhaustive search: %#v %v", exhaustive, err)
			}
		})
	}
}

func TestRepoSearchIndexFlagsScopedToSearchCorpus(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "visible.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "generated", "hidden.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "node_modules", "ignored.txt"), "needle\n")
	commitAll(t, dir, "initial")
	opts := Options{CacheRoot: t.TempDir(), ExcludeDirs: []string{"generated"}}
	if report, err := Search(context.Background(), dir, "needle", opts); err != nil || report.CacheStatus != CacheStatusRebuilt {
		t.Fatalf("initial search: %#v %v", report, err)
	}
	gitRun(t, dir, "update-index", "--assume-unchanged", "generated/hidden.txt")
	gitRun(t, dir, "update-index", "--skip-worktree", "node_modules/ignored.txt")
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

func TestRepoSearchIndexFlagsCannotBeHiddenByPostIndexQueryScopes(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "visible", "doc.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "other", "flagged.txt"), "unrelated\n")
	commitAll(t, dir, "initial")
	gitRun(t, dir, "update-index", "--assume-unchanged", "other/flagged.txt")
	for _, opts := range []Options{
		{DisableCache: true, PathPrefixes: []string{"visible"}},
		{DisableCache: true, Symbols: []string{"needle"}},
	} {
		if report, err := Search(context.Background(), dir, "needle", opts); !errors.Is(err, ErrUnsupportedIndexFlags) {
			t.Fatalf("post-index query scope hid flagged entry: %#v %v", report, err)
		}
	}
}

func TestRepoSearchRejectsMalformedTrackedIndexEntryInsteadOfEmptyCorpus(t *testing.T) {
	for _, raw := range []string{"?", "H malformed", "X 100644 deadbeef 0\tentry.txt"} {
		if entry, included, err := parseTrackedIndexEntry(raw, map[string]bool{}); err == nil || included {
			t.Fatalf("malformed index entry became search result: raw=%q entry=%#v included=%v err=%v", raw, entry, included, err)
		}
	}
}
func TestRepoSearchExhaustiveRejectsFlagAppearingDuringScan(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "data.txt"), "needle\n")
	commitAll(t, dir, "initial")
	original := captureFingerprint
	calls := 0
	captureFingerprint = func(ctx context.Context, root string, excludeDirs map[string]bool) (fingerprint, error) {
		calls++
		if calls == 2 {
			gitRun(t, dir, "update-index", "--skip-worktree", "data.txt")
		}
		return original(ctx, root, excludeDirs)
	}
	defer func() { captureFingerprint = original }()
	if report, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{}); !errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("exhaustive search claimed completion after flag appeared: %#v %v", report, err)
	}
	if calls < 2 {
		t.Fatal("exhaustive search did not reach its post-search fingerprint")
	}
}
