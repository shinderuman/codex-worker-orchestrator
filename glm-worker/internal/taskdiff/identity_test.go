package taskdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newIdentityRepo(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repoRoot, "mode-change.txt"), "same content\n")
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-qm", "baseline")
	return repoRoot
}

func TestFileIdentityDetectsModeOnlyChange(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	before, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repoRoot, "mode-change.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if SameFileIdentity(before[0], after[0]) {
		t.Fatalf("mode-only change kept identity: %#v", after[0])
	}
}

func TestFileIdentityDetectsUnmergedStageChange(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	writeFile(t, filepath.Join(repoRoot, "mode-change.txt"), "main side\n")
	runGit(t, repoRoot, "commit", "-qam", "main line")
	runGit(t, repoRoot, "checkout", "-qb", "side")
	writeFile(t, filepath.Join(repoRoot, "mode-change.txt"), "branch side\n")
	runGit(t, repoRoot, "commit", "-qam", "side line")
	runGit(t, repoRoot, "checkout", "-q", "-")
	writeFile(t, filepath.Join(repoRoot, "mode-change.txt"), "main conflict\n")
	runGit(t, repoRoot, "commit", "-qam", "main conflict")
	merge := exec.Command("git", "-C", repoRoot, "merge", "side")
	if output, err := merge.CombinedOutput(); err != nil && !strings.Contains(string(output), "CONFLICT") {
		t.Fatalf("git merge side: %v: %s", err, output)
	}
	if unmerged := runGit(t, repoRoot, "ls-files", "-u"); !strings.Contains(unmerged, "mode-change.txt") {
		t.Fatalf("merge conflict did not produce unmerged stages: %q", unmerged)
	}

	unmerged, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatal(err)
	}
	stageThree := unmerged[0].IndexDigest
	runGit(t, repoRoot, "merge", "--abort")
	resolved, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if stageThree == "" || resolved[0].IndexDigest == "" {
		t.Fatalf("index digests must be present: unmerged=%#v resolved=%#v", unmerged[0], resolved[0])
	}
	if stageThree == resolved[0].IndexDigest {
		t.Fatal("unmerged multi-stage index kept the same digest as the single-stage index")
	}
}

func TestFileIdentitiesAllowDeletedPath(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	before, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repoRoot, "mode-change.txt")); err != nil {
		t.Fatal(err)
	}
	after, err := FileIdentities(repoRoot, []string{"mode-change.txt"})
	if err != nil {
		t.Fatalf("deleted path must not fail FileIdentities: %v", err)
	}
	if after[0].WorktreeDigest != "" {
		t.Fatalf("deleted path worktree digest = %q want empty", after[0].WorktreeDigest)
	}
	if SameFileIdentity(before[0], after[0]) {
		t.Fatalf("deleted path kept identity: %#v", after[0])
	}
}

func TestFileIdentitiesAllowDeletedNestedPath(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	nested := filepath.Join(repoRoot, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, nested, "guide body\n")
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-qm", "nested")
	if err := os.RemoveAll(filepath.Join(repoRoot, "docs")); err != nil {
		t.Fatal(err)
	}
	identities, err := FileIdentities(repoRoot, []string{"docs/guide.md"})
	if err != nil {
		t.Fatalf("deleted nested path must not fail FileIdentities: %v", err)
	}
	if identities[0].WorktreeDigest != "" {
		t.Fatalf("deleted nested identity = %#v", identities[0])
	}
}

func TestFileIdentityDigestsIndependentOfQuotePathConfig(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	nonASCII := filepath.Join(repoRoot, "資料", "案内.md")
	if err := os.MkdirAll(filepath.Dir(nonASCII), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, nonASCII, "non-ascii path body\n")
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-qm", "non-ascii path")

	runGit(t, repoRoot, "config", "core.quotepath", "true")
	quoted, err := FileIdentities(repoRoot, []string{"資料/案内.md"})
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "config", "core.quotepath", "false")
	raw, err := FileIdentities(repoRoot, []string{"資料/案内.md"})
	if err != nil {
		t.Fatal(err)
	}
	if quoted[0].IndexDigest == "" || quoted[0].IndexDigest != raw[0].IndexDigest {
		t.Fatalf("index digest depends on quote config: quoted=%q raw=%q", quoted[0].IndexDigest, raw[0].IndexDigest)
	}
	if quoted[0].HeadDigest == "" || quoted[0].HeadDigest != raw[0].HeadDigest {
		t.Fatalf("head digest depends on quote config: quoted=%q raw=%q", quoted[0].HeadDigest, raw[0].HeadDigest)
	}
}

func TestFileIdentityDetectsSymlinkTargetChange(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	if err := os.Symlink("mode-change.txt", filepath.Join(repoRoot, "guide-link.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-qm", "symlink")
	before, err := FileIdentities(repoRoot, []string{"guide-link.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repoRoot, "guide-link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other-target.md", filepath.Join(repoRoot, "guide-link.md")); err != nil {
		t.Fatal(err)
	}
	after, err := FileIdentities(repoRoot, []string{"guide-link.md"})
	if err != nil {
		t.Fatal(err)
	}
	if before[0].WorktreeDigest == "" {
		t.Fatalf("symlink worktree digest missing: %#v", before[0])
	}
	if SameFileIdentity(before[0], after[0]) {
		t.Fatalf("symlink target change kept identity: %#v", after[0])
	}
}

func TestFileIdentitiesRejectPathBeyondRepository(t *testing.T) {
	repoRoot := newIdentityRepo(t)
	if _, err := FileIdentities(repoRoot, []string{"../escape.txt"}); err == nil {
		t.Fatal("relative escape path must be rejected")
	}
	writeFile(t, filepath.Join(filepath.Dir(repoRoot), "outside.md"), "outside body\n")
	if err := os.Symlink("../outside.md", filepath.Join(repoRoot, "escape-link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := FileIdentities(repoRoot, []string{"escape-link.md"}); err == nil {
		t.Fatal("symlink escape path must be rejected")
	}
}

func TestRepositoryContainmentHandlesFilesystemRootAndSiblings(t *testing.T) {
	volumeRoot := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	repo := filepath.Join(volumeRoot, "repo")
	for _, tc := range []struct {
		name string
		root string
		path string
		want bool
	}{
		{"filesystem-root", volumeRoot, filepath.Join(volumeRoot, "tracked.md"), true},
		{"root-itself", volumeRoot, volumeRoot, true},
		{"nested", repo, filepath.Join(repo, "dir", "tracked.md"), true},
		{"prefix-sibling", repo, filepath.Join(volumeRoot, "repo-other", "tracked.md"), false},
		{"parent", repo, volumeRoot, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinRepoRoot(tc.root, tc.path); got != tc.want {
				t.Fatalf("containment root=%q path=%q: got %v, want %v", tc.root, tc.path, got, tc.want)
			}
		})
	}
}
