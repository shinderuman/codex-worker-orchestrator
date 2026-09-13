package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentActionHeadAuthorityRejectsExistingNonCommitBranchRef(t *testing.T) {
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")

	blobPath := filepath.Join(repo, "blob.txt")
	if err := os.WriteFile(blobPath, []byte("not a commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blobOID := strings.TrimSpace(pushBindingGitOutput(t, repo, "hash-object", "-w", "blob.txt"))
	branch := strings.TrimSpace(pushBindingGitOutput(t, repo, "symbolic-ref", "--short", "HEAD"))
	refPath := filepath.Join(repo, ".git", "refs", "heads", branch)
	if err := os.MkdirAll(filepath.Dir(refPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refPath, []byte(blobOID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	head, unborn, failure := completeHeadState(repo)
	if failure == nil || failure.Reason != completeTargetHeadUnresolvable || head != "" || unborn {
		t.Fatalf("completion HEAD classification = head:%q unborn:%t failure:%#v", head, unborn, failure)
	}

	target, failure := pushBindingTargetFromRepo(repo)
	if target != nil || failure == nil || failure.Reason != completeTargetHeadUnresolvable {
		t.Fatalf("push-binding HEAD classification = target:%#v failure:%#v", target, failure)
	}

	if summary, err := readFinalizationGitSummary(repo); err == nil {
		t.Fatalf("finalization accepted non-commit branch ref: %#v", summary)
	}
}

func TestParentActionHeadAuthorityPreservesProvenUnbornSurfaceSemantics(t *testing.T) {
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")

	head, unborn, failure := completeHeadState(repo)
	if failure != nil || head != "" || !unborn {
		t.Fatalf("completion unborn classification = head:%q unborn:%t failure:%#v", head, unborn, failure)
	}

	target, failure := pushBindingTargetFromRepo(repo)
	if target != nil || failure == nil || failure.Reason != completeTargetHeadUnresolvable {
		t.Fatalf("push-binding unborn classification = target:%#v failure:%#v", target, failure)
	}

	if summary, err := readFinalizationGitSummary(repo); err == nil {
		t.Fatalf("finalization accepted unborn HEAD: %#v", summary)
	}
}
