package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizationGitSummaryRejectsExistingNonCommitBranchRef(t *testing.T) {
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")

	blobPath := filepath.Join(repo, "blob.txt")
	if err := os.WriteFile(blobPath, []byte("not a commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blobOIDOutput, err := gitFinalizationOutput(repo, "hash-object", "-w", "blob.txt")
	if err != nil {
		t.Fatal(err)
	}
	branchOutput, err := gitFinalizationOutput(repo, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	blobOID := strings.TrimSpace(blobOIDOutput)
	branch := strings.TrimSpace(branchOutput)
	refPath := filepath.Join(repo, ".git", "refs", "heads", branch)
	if err := os.MkdirAll(filepath.Dir(refPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refPath, []byte(blobOID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if summary, err := readFinalizationGitSummary(repo); err == nil {
		t.Fatalf("finalization accepted non-commit branch ref: %#v", summary)
	}
}

func TestFinalizationGitSummaryRejectsUnbornHEAD(t *testing.T) {
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")

	if summary, err := readFinalizationGitSummary(repo); err == nil {
		t.Fatalf("finalization accepted unborn HEAD: %#v", summary)
	}
}
