package parentevidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestDeletedGoSymbolUsesCanonicalDiffEvidence(t *testing.T) {
	repoRoot := t.TempDir()
	runDeletedSymbolGit(t, repoRoot, "init", "-q")
	runDeletedSymbolGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDeletedSymbolGit(t, repoRoot, "config", "user.name", "Test")
	path := filepath.Join(repoRoot, "removed.go")
	if err := os.WriteFile(path, []byte("package removed\n\nfunc Removed() int {\n\treturn 1\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDeletedSymbolGit(t, repoRoot, "add", "removed.go")
	runDeletedSymbolGit(t, repoRoot, "commit", "-qm", "add removed symbol")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	target, err := reviewtarget.ParseTarget("removed.go:Removed")
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewtarget.ValidateProofAddressable(repoRoot, target); err != nil {
		t.Fatalf("deleted symbol should remain proof-addressable: %v", err)
	}

	manifest := Manifest{}
	diffPaths := map[string]struct{}{}
	if err := appendReviewManifestTarget(repoRoot, &manifest, map[string]struct{}{}, diffPaths, "inspect deletion", target); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Source) != 0 {
		t.Fatalf("deleted symbol emitted source request: %#v", manifest.Source)
	}
	if _, ok := diffPaths["removed.go"]; !ok {
		t.Fatalf("deleted symbol did not request diff evidence: %#v", diffPaths)
	}

	body := runDeletedSymbolGitOutput(t, repoRoot, "diff", "HEAD", "--no-ext-diff", "--no-renames", "--", "removed.go")
	diff := DiffBody{
		Paths: []string{"removed.go"},
		Files: []DiffFile{{Path: "removed.go", Status: "deleted", HeadBlob: "head"}},
		Body:  body,
	}
	if !ReviewDiffCoversTarget("removed.go:Removed", diff) {
		t.Fatalf("deleted symbol was not covered by its canonical full-file deletion diff:\n%s", body)
	}
	if ReviewDiffCoversTarget("removed.go:Other", diff) {
		t.Fatal("deletion diff incorrectly covered a symbol that did not exist")
	}
}

func runDeletedSymbolGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func runDeletedSymbolGitOutput(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(output)
}
