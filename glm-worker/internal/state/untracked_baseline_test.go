package state

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestCaptureGitBaselineStoresPrivateUntrackedSnapshot(t *testing.T) {
	repoRoot, cfg, st := newUntrackedBaselineStateFixture(t, "private")
	if err := os.WriteFile(filepath.Join(repoRoot, "preexisting.txt"), []byte("private contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(repoRoot, "preexisting-link")); err != nil {
		t.Fatal(err)
	}

	if err := CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	entries, err := st.ReadUntrackedBaseline()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	byPath := make(map[string]UntrackedBaselineEntry, len(entries))
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	if got := string(byPath["preexisting.txt"].Content); got != "private contents\n" {
		t.Fatalf("regular content = %q", got)
	}
	if byPath["preexisting.txt"].Kind != UntrackedBaselineKindFile {
		t.Fatalf("regular kind = %q", byPath["preexisting.txt"].Kind)
	}
	if got := string(byPath["preexisting-link"].Content); got != "target.txt" {
		t.Fatalf("symlink target = %q", got)
	}
	if byPath["preexisting-link"].Kind != UntrackedBaselineKindSymlink {
		t.Fatalf("symlink kind = %q", byPath["preexisting-link"].Kind)
	}

	root := st.Path(baselineUntrackedFile)
	assertPrivateMode(t, root, 0o700)
	assertPrivateMode(t, filepath.Join(root, untrackedBaselineBlobDir), 0o700)
	assertPrivateMode(t, filepath.Join(root, untrackedBaselineManifestFile), 0o600)
	blobEntries, err := os.ReadDir(filepath.Join(root, untrackedBaselineBlobDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(blobEntries) != 2 {
		t.Fatalf("blob entries = %d, want 2", len(blobEntries))
	}
	for _, entry := range blobEntries {
		assertPrivateMode(t, filepath.Join(root, untrackedBaselineBlobDir, entry.Name()), 0o600)
	}

	evidence := st.BaselineEvidence()
	if evidence == nil || evidence.Untracked != root {
		t.Fatalf("baseline evidence = %#v", evidence)
	}
	if strings.Contains(evidence.Untracked, untrackedBaselineBlobDir+string(filepath.Separator)) {
		t.Fatalf("baseline evidence exposed blob path: %q", evidence.Untracked)
	}
}

func TestCaptureGitBaselineRejectsOversizedUntrackedPreimage(t *testing.T) {
	repoRoot, cfg, st := newUntrackedBaselineStateFixture(t, "oversized")
	data := bytes.Repeat([]byte{'x'}, untrackedBaselineMaxEntryBytes+1)
	if err := os.WriteFile(filepath.Join(repoRoot, "large.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	err := CaptureGitBaseline(cfg, st)
	if err == nil || !strings.Contains(err.Error(), "per-entry limit") {
		t.Fatalf("oversized baseline error = %v", err)
	}
	for _, name := range []string{"baseline-head", "baseline-status", "baseline-worktree.patch", "baseline-index.patch", baselineUntrackedFile} {
		if st.Exists(name) {
			t.Fatalf("partial baseline state survived failed capture: %s", name)
		}
	}
}

func TestReadUntrackedBaselineRejectsMissingBlob(t *testing.T) {
	repoRoot, cfg, st := newUntrackedBaselineStateFixture(t, "missing-blob")
	if err := os.WriteFile(filepath.Join(repoRoot, "preexisting.txt"), []byte("contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(st.Path(baselineUntrackedFile), untrackedBaselineBlobDir)); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadUntrackedBaseline(); err == nil || !strings.Contains(err.Error(), "blob") {
		t.Fatalf("missing blob was not rejected: %v", err)
	}
}

func TestReadUntrackedBaselineRejectsEscapingManifestPath(t *testing.T) {
	_, cfg, st := newUntrackedBaselineStateFixture(t, "escaping-path")
	if err := CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"entries":[{"path":"../outside","kind":"file","mode":384,"size":0,"sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","blob":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}]}` + "\n"
	if err := os.WriteFile(filepath.Join(st.Path(baselineUntrackedFile), untrackedBaselineManifestFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadUntrackedBaseline(); err == nil || !strings.Contains(err.Error(), "invalid untracked baseline path") {
		t.Fatalf("escaping manifest path was not rejected: %v", err)
	}
}

func newUntrackedBaselineStateFixture(t *testing.T, suffix string) (string, config.AppConfig, *StateStore) {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runStateGit(t, repoRoot, "init", "-q")
	runStateGit(t, repoRoot, "config", "user.email", "test@example.com")
	runStateGit(t, repoRoot, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoRoot, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runStateGit(t, repoRoot, "add", "seed.txt")
	runStateGit(t, repoRoot, "commit", "-qm", "baseline")
	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  "untracked-baseline-" + suffix,
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return repoRoot, cfg, st
}

func runStateGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
