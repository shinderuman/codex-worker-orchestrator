package reposearch

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)


func TestRepoSearchDistinguishesRealUnmergedIndexStages(t *testing.T) {
	dir := initRepo(t)
	conflict := filepath.Join(dir, "conflict.txt")
	writeTestFile(t, conflict, "base\n")
	commitAll(t, dir, "base")
	opts := Options{CacheRoot: t.TempDir()}
	if report, err := Search(context.Background(), dir, "base", opts); err != nil || report.CacheStatus != CacheStatusRebuilt {
		t.Fatalf("initial cache build: %#v %v", report, err)
	}
	gitRun(t, dir, "checkout", "-b", "conflict-side")
	writeTestFile(t, conflict, "side\n")
	commitAll(t, dir, "side")
	gitRun(t, dir, "checkout", "-")
	writeTestFile(t, conflict, "main\n")
	commitAll(t, dir, "main")

	merge := exec.Command("git", "-C", dir, "merge", "--no-edit", "conflict-side")
	if output, err := merge.CombinedOutput(); err == nil {
		t.Fatalf("expected merge conflict; git merge output: %s", output)
	}

	output, err := gitOutput(context.Background(), dir, "ls-files", "-z", "-v", "-s", "--cached")
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]bool{}
	for _, raw := range strings.Split(string(output), "\x00") {
		if raw == "" {
			continue
		}
		tab := strings.IndexByte(raw, '\t')
		if len(raw) < 3 || tab < 0 || raw[0] != 'M' {
			t.Fatalf("unexpected conflict index entry: %q", raw)
		}
		fields := strings.Fields(raw[2:tab])
		if len(fields) != 3 || fields[0] != trackedModeRegular {
			t.Fatalf("unexpected stage header: %q", raw)
		}
		stages[fields[2]] = true
		if entry, include, err := parseTrackedIndexEntry(raw, map[string]bool{}); include ||
			!errors.Is(err, ErrUnmergedIndexEntry) || errors.Is(err, ErrUnsupportedIndexFlags) ||
			!strings.Contains(err.Error(), "conflict.txt") {
			t.Fatalf("stage %s misdiagnosed: %#v %v %v", fields[2], entry, include, err)
		}
	}
	if !reflect.DeepEqual(stages, map[string]bool{"1": true, "2": true, "3": true}) {
		t.Fatalf("unmerged stages = %v want 1, 2, 3", stages)
	}
	for _, test := range []struct {
		name string
		opts Options
	}{
		{name: "cache previously populated", opts: opts},
		{name: "cache disabled", opts: Options{DisableCache: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Search(context.Background(), dir, "base", test.opts)
			if !errors.Is(err, ErrUnmergedIndexEntry) || errors.Is(err, ErrUnsupportedIndexFlags) {
				t.Fatalf("unmerged index returned result or wrong error: %#v %v", report, err)
			}
		})
	}
	if report, err := ExhaustiveSearch(context.Background(), dir, "base", ExhaustiveOptions{}); 
		!errors.Is(err, ErrUnmergedIndexEntry) || errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("exhaustive unmerged index returned result or wrong error: %#v %v", report, err)
	}
}

func TestRepoSearchUnmergedEntryIsNotHiddenByUnsupportedMode(t *testing.T) {
	raw := "M 120000 0000000000000000000000000000000000000001 2\tlink.txt"
	if entry, include, err := parseTrackedIndexEntry(raw, map[string]bool{}); include ||
		!errors.Is(err, ErrUnmergedIndexEntry) || errors.Is(err, ErrUnsupportedIndexFlags) {
		t.Fatalf("unmerged symlink stage silently skipped: %#v %v %v", entry, include, err)
	}
	if _, include, err := parseTrackedIndexEntry(raw, map[string]bool{"generated": true}); include ||
		!errors.Is(err, ErrUnmergedIndexEntry) {
		t.Fatalf("unexcluded unmerged entry: include=%v err=%v", include, err)
	}
	excluded := "M 120000 0000000000000000000000000000000000000001 2\tgenerated/link.txt"
	if _, include, err := parseTrackedIndexEntry(excluded, map[string]bool{"generated": true}); include || err != nil {
		t.Fatalf("excluded path should remain out of corpus: include=%v err=%v", include, err)
	}
}

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
