package reposearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExhaustiveSearchScansEntireSearchableCorpusWithoutTopN(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "needle first\n")
	writeTestFile(t, filepath.Join(dir, "nested", "b.txt"), "second needle\n")
	writeTestFile(t, filepath.Join(dir, "needle-name.txt"), "other\n")
	writeTestFile(t, filepath.Join(dir, "miss.txt"), "nothing\n")
	commitAll(t, dir, "init")

	report, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{MaxMatches: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report.Predicate != exhaustivePredicate || report.EnumeratedFiles != 4 || report.ScannedFiles != 4 || report.SkippedFiles != 0 {
		t.Fatalf("report=%#v", report)
	}
	got := make([]string, 0, len(report.Matches))
	for _, match := range report.Matches {
		got = append(got, match.Path)
	}
	want := []string{"a.txt", "needle-name.txt", "nested/b.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches=%v want=%v", got, want)
	}
}

func TestExhaustiveSearchIncludesUntrackedSearchableFiles(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "needle\n")
	commitAll(t, dir, "init")
	writeTestFile(t, filepath.Join(dir, "untracked.txt"), "needle\n")

	report, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.EnumeratedFiles != 2 || len(report.Matches) != 2 {
		t.Fatalf("report=%#v", report)
	}
}

func TestExhaustiveSearchFailsInsteadOfTruncatingMatches(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "needle\n")
	commitAll(t, dir, "init")

	_, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{MaxMatches: 1})
	if !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("err=%v want ErrIndexLimit", err)
	}
}

func TestExhaustiveSearchReportsExcludedBinaryCorpus(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "text.txt"), "needle\n")
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{'n', 0, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "init")

	report, err := ExhaustiveSearch(context.Background(), dir, "needle", ExhaustiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.EnumeratedFiles != 2 || report.ScannedFiles != 1 || report.SkippedFiles != 1 {
		t.Fatalf("report=%#v", report)
	}
}
