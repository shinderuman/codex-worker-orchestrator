package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	path := st.Path(baselineUntrackedFile)
	assertPrivateMode(t, path, 0o600)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private contents")) {
		t.Fatalf("snapshot unexpectedly stores raw text inline: %s", raw)
	}
	evidence := st.BaselineEvidence()
	if evidence == nil || evidence.Untracked != path {
		t.Fatalf("baseline evidence = %#v", evidence)
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

func TestReadUntrackedBaselineRejectsCorruptContentDigest(t *testing.T) {
	repoRoot, cfg, st := newUntrackedBaselineStateFixture(t, "corrupt")
	if err := os.WriteFile(filepath.Join(repoRoot, "preexisting.txt"), []byte("contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.Path(baselineUntrackedFile))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot untrackedBaselineSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %#v", snapshot.Entries)
	}
	snapshot.Entries[0].Content[0] ^= 1
	corrupt, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(baselineUntrackedFile), append(corrupt, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadUntrackedBaseline(); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("corrupt snapshot was not rejected: %v", err)
	}
}

func TestReadUntrackedBaselineRejectsEscapingSnapshotPath(t *testing.T) {
	_, cfg, st := newUntrackedBaselineStateFixture(t, "escaping-path")
	if err := CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(nil)
	snapshot := untrackedBaselineSnapshot{Version: untrackedBaselineVersion, Entries: []untrackedBaselineSnapshotEntry{{
		Path:    "../outside",
		Kind:    UntrackedBaselineKindFile,
		Mode:    0o600,
		Size:    0,
		SHA256:  hex.EncodeToString(digest[:]),
		Content: nil,
	}}}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(baselineUntrackedFile), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadUntrackedBaseline(); err == nil || !strings.Contains(err.Error(), "invalid untracked baseline path") {
		t.Fatalf("escaping snapshot path was not rejected: %v", err)
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
