package reposearch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mutateDuringCapture(t *testing.T, dir string, mutateAt func(call int) bool) {
	t.Helper()
	original := captureFingerprint
	call := 0
	captureFingerprint = func(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (fingerprint, error) {
		call++
		if mutateAt(call) {
			writeTestFile(t, filepath.Join(dir, "raced.txt"), fmt.Sprintf("needle raced %d\n", call))
		}
		return original(ctx, repoRoot, excludeDirs)
	}
	t.Cleanup(func() { captureFingerprint = original })
}

func TestSearchRetriesOnceOnMidSearchMutation(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
	commitAll(t, dir, "init")

	mutateDuringCapture(t, dir, func(call int) bool { return call == 2 })

	report, err := Search(context.Background(), dir, "needle", Options{CacheRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultPaths(report); !reflect.DeepEqual(got, []string{"a.txt", "raced.txt"}) {
		t.Fatalf("results = %v want 変化後の状態を反映した [a.txt raced.txt]", got)
	}
}

func TestSearchFailsClosedOnRepeatedMutation(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
	commitAll(t, dir, "init")

	mutateDuringCapture(t, dir, func(call int) bool { return call == 2 || call == 5 })

	if _, err := Search(context.Background(), dir, "needle", Options{CacheRoot: t.TempDir()}); !errors.Is(err, ErrIndexRace) {
		t.Fatalf("error = %v want ErrIndexRace", err)
	}
}

func TestSearchRaceDoesNotWriteMixedCache(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
	commitAll(t, dir, "init")
	cacheRoot := t.TempDir()
	mutateDuringCapture(t, dir, func(call int) bool { return call == 2 || call == 5 })

	if _, err := Search(context.Background(), dir, "needle", Options{CacheRoot: cacheRoot}); !errors.Is(err, ErrIndexRace) {
		t.Fatalf("error = %v want ErrIndexRace", err)
	}
	if _, err := os.Stat(canonicalCachePath(t, cacheRoot, dir)); !os.IsNotExist(err) {
		t.Fatal("race失敗時にcacheが書かれています")
	}
}
