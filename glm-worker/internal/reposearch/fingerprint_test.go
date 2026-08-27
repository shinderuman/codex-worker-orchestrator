package reposearch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type freshnessCase struct {
	name string

	setup func(t *testing.T, dir string)

	mutate func(t *testing.T, dir string)

	expectRebuilt *bool

	wantPaths []string
}

func rebuiltExpected() *bool {
	rebuilt := true
	return &rebuilt
}

func hitExpected() *bool {
	rebuilt := false
	return &rebuilt
}

func trackedFreshnessCases() []freshnessCase {
	return []freshnessCase{
		{
			name: "tracked searchable content change rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle changed\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "tracked searchable deletion rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				writeTestFile(t, filepath.Join(dir, "b.txt"), "needle two\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
					t.Fatal(err)
				}
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"b.txt"},
		},
		{
			name: "tracked file named like exclude dir stays in corpus",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "vendor"), "needle regular file\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "vendor"), "needle changed\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"vendor"},
		},
		{
			name: "tracked change under default excluded dir keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "real.txt"), "needle\n")
				writeTestFile(t, filepath.Join(dir, "vendor", "lib.go"), "needle vendored\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "vendor", "lib.go"), "needle vendored changed\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"real.txt"},
		},
		{
			name: "tracked change under nested excluded dir never returns stale",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "real.txt"), "needle\n")
				writeTestFile(t, filepath.Join(dir, "sub", "node_modules", "x.js"), "needle\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "sub", "node_modules", "x.js"), "needle changed\n")
			},
			expectRebuilt: nil,
			wantPaths:     []string{"real.txt"},
		},
		{
			name: "tracked binary change never returns stale",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "text.txt"), "needle\n")
				writeTestFile(t, filepath.Join(dir, "data.bin"), "needle\x00payload one\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "data.bin"), "needle\x00payload two\n")
			},
			expectRebuilt: nil,
			wantPaths:     []string{"text.txt"},
		},
	}
}

func untrackedFreshnessCases() []freshnessCase {
	return []freshnessCase{
		{
			name: "untracked searchable add rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.txt"), "needle untracked\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt", "u.txt"},
		},
		{
			name: "untracked searchable content change rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				writeTestFile(t, filepath.Join(dir, "u.txt"), "needle untracked\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.txt"), "needle changed\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt", "u.txt"},
		},
		{
			name: "untracked searchable removal rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				writeTestFile(t, filepath.Join(dir, "u.txt"), "needle untracked\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "u.txt")); err != nil {
					t.Fatal(err)
				}
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "untracked binary rewrite keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				writeTestFile(t, filepath.Join(dir, "u.bin"), "needle\x00payload one\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.bin"), "needle\x00payload two\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "untracked binary to text rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				writeTestFile(t, filepath.Join(dir, "u.bin"), "needle\x00payload one\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.bin"), "needle now text\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt", "u.bin"},
		},
		{
			name: "untracked oversize rewrite keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				writeTestFile(t, filepath.Join(dir, "u.log"), "needle "+strings.Repeat("x", maxFileBytes)+"\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.log"), "needle "+strings.Repeat("y", maxFileBytes)+"\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "untracked oversize to searchable rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				writeTestFile(t, filepath.Join(dir, "u.log"), "needle "+strings.Repeat("x", maxFileBytes)+"\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "u.log"), "needle small now\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt", "u.log"},
		},
		{
			name: "untracked symlink retarget keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				writeTestFile(t, filepath.Join(dir, "target.txt"), "x\n")
				commitAll(t, dir, "init")
				if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "link.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("a.txt", filepath.Join(dir, "link.txt")); err != nil {
					t.Fatal(err)
				}
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "untracked symlink to regular rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				if err := os.Symlink("a.txt", filepath.Join(dir, "link.txt")); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "link.txt")); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(dir, "link.txt"), "needle regular now\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt", "link.txt"},
		},
	}
}

func excludedFreshnessCases() []freshnessCase {
	return []freshnessCase{
		{
			name: "untracked file under default excluded dir keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "node_modules", "u.js"), "needle ignored corpus\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "nested untracked repo appears without error and keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
			},
			mutate: func(t *testing.T, dir string) {
				nested := filepath.Join(dir, "inner")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "", "init", "--quiet", nested)
				writeTestFile(t, filepath.Join(nested, "b.txt"), "needle nested\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "nested untracked repo removal keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				nested := filepath.Join(dir, "inner")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "", "init", "--quiet", nested)
				writeTestFile(t, filepath.Join(nested, "b.txt"), "needle nested\n")
			},
			mutate: func(t *testing.T, dir string) {
				if err := os.RemoveAll(filepath.Join(dir, "inner")); err != nil {
					t.Fatal(err)
				}
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "nested untracked repo content change keeps cache",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				nested := filepath.Join(dir, "inner")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "", "init", "--quiet", nested)
				writeTestFile(t, filepath.Join(nested, "b.txt"), "needle nested\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "inner", "b.txt"), "needle nested changed\n")
				writeTestFile(t, filepath.Join(dir, "inner", "c.txt"), "needle nested added\n")
			},
			expectRebuilt: hitExpected(),
			wantPaths:     []string{"a.txt"},
		},
		{
			name: "gitignore hiding untracked searchable file rebuilds",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "a.txt"), "needle one\n")
				commitAll(t, dir, "init")
				writeTestFile(t, filepath.Join(dir, "u.txt"), "needle untracked\n")
			},
			mutate: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, ".gitignore"), "u.txt\n")
			},
			expectRebuilt: rebuiltExpected(),
			wantPaths:     []string{"a.txt"},
		},
	}
}

func runFreshnessCases(t *testing.T, cases []freshnessCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			tt.setup(t, dir)
			opts := Options{CacheRoot: t.TempDir()}

			if first := searchNeedle(t, dir, opts); first.CacheStatus != CacheStatusRebuilt {
				t.Fatalf("初回status = %q want rebuilt", first.CacheStatus)
			}
			tt.mutate(t, dir)
			report := searchNeedle(t, dir, opts)
			fresh := searchNeedle(t, dir, Options{DisableCache: true})

			if !reflect.DeepEqual(report.Results, fresh.Results) {
				t.Fatalf("cache結果と新規検索が不一致(staleまたは誤反映):\ncache: %v\nfresh: %v", report.Results, fresh.Results)
			}
			if tt.expectRebuilt != nil {
				wantStatus := CacheStatusHit
				if *tt.expectRebuilt {
					wantStatus = CacheStatusRebuilt
				}
				if report.CacheStatus != wantStatus {
					t.Fatalf("status = %q want %q", report.CacheStatus, wantStatus)
				}
			}
			if got := resultPaths(report); !reflect.DeepEqual(got, tt.wantPaths) {
				t.Fatalf("results = %v want %v", got, tt.wantPaths)
			}
		})
	}
}

func TestSearchFreshnessTracksTrackedCorpusBoundaries(t *testing.T) {
	runFreshnessCases(t, trackedFreshnessCases())
}

func TestSearchFreshnessTracksUntrackedCorpusBoundaries(t *testing.T) {
	runFreshnessCases(t, untrackedFreshnessCases())
}

func TestSearchFreshnessTracksExcludedCorpusBoundaries(t *testing.T) {
	runFreshnessCases(t, excludedFreshnessCases())
}

func TestSearchFreshnessIgnoresUserExcludedDirContent(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "real.txt"), "needle\n")
	writeTestFile(t, filepath.Join(dir, "generated", "g.txt"), "needle generated\n")
	commitAll(t, dir, "init")
	opts := Options{CacheRoot: t.TempDir(), ExcludeDirs: []string{"generated"}}

	if first := searchNeedle(t, dir, opts); first.CacheStatus != CacheStatusRebuilt {
		t.Fatalf("初回status = %q want rebuilt", first.CacheStatus)
	}
	writeTestFile(t, filepath.Join(dir, "generated", "g.txt"), "needle generated changed\n")
	report := searchNeedle(t, dir, opts)
	if report.CacheStatus != CacheStatusHit {
		t.Fatalf("追加除外directory配下の変更後status = %q want hit", report.CacheStatus)
	}
	if got := resultPaths(report); !reflect.DeepEqual(got, []string{"real.txt"}) {
		t.Fatalf("results = %v want [real.txt]", got)
	}
}

func TestSearchTreatsSubmoduleOutsideCorpus(t *testing.T) {
	dir := initRepo(t)
	writeTestFile(t, filepath.Join(dir, "real.txt"), "needle\n")
	commitAll(t, dir, "init")
	subSource := t.TempDir()
	gitRun(t, "", "init", "--quiet", "--initial-branch=main", subSource)
	writeTestFile(t, filepath.Join(subSource, "s.txt"), "needle inside submodule\n")
	commitAll(t, subSource, "sub init")

	gitRun(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "--quiet", subSource, "deps/subm")
	commitAll(t, dir, "add submodule")
	opts := Options{CacheRoot: t.TempDir()}

	first := searchNeedle(t, dir, opts)
	if got := resultPaths(first); !reflect.DeepEqual(got, []string{"real.txt"}) {
		t.Fatalf("results = %v want submodule配下を除く [real.txt]", got)
	}

	writeTestFile(t, filepath.Join(dir, "deps", "subm", "s2.txt"), "needle second\n")
	gitRun(t, filepath.Join(dir, "deps", "subm"), "add", "-A")
	gitRun(t, filepath.Join(dir, "deps", "subm"), "commit", "--quiet", "-m", "s2")
	gitRun(t, dir, "add", "deps/subm")

	report := searchNeedle(t, dir, opts)
	fresh := searchNeedle(t, dir, Options{DisableCache: true})
	if !reflect.DeepEqual(report.Results, fresh.Results) {
		t.Fatalf("cache結果と新規検索が不一致:\ncache: %v\nfresh: %v", report.Results, fresh.Results)
	}
	if got := resultPaths(report); !reflect.DeepEqual(got, []string{"real.txt"}) {
		t.Fatalf("results = %v want submodule内部を含まない [real.txt]", got)
	}
}
