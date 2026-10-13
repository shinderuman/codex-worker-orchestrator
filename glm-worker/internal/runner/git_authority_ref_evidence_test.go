package runner

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGitAuthorityGuardRetainsConcreteRefMutationEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := newGitAuthorityRepo(t)
	beforeDigest, err := CaptureGitAuthorityRefDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	currentBranch := strings.TrimSpace(runGitAuthorityOutput(t, root, "symbolic-ref", "--short", "HEAD"))
	guarded, _ := newGitAuthorityProductionRunner(t, root, "\"$REAL_GIT\" branch -m "+currentBranch+" bypassed", map[string]string{"REAL_GIT": realGit})
	_, err = guarded.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "after-call-mutation" {
		t.Fatalf("guard error = %#v", err)
	}
	if guardErr.RefBeforeDigest != beforeDigest || guardErr.RefAfterDigest == "" || guardErr.RefAfterDigest == beforeDigest {
		t.Fatalf("ref digests = before:%q after:%q want before:%q", guardErr.RefBeforeDigest, guardErr.RefAfterDigest, beforeDigest)
	}
	if guardErr.RefChangesTruncated {
		t.Fatal("single authority rename must not be truncated")
	}
	names := map[string]bool{}
	for _, change := range guardErr.RefChanges {
		names[change.Name] = true
	}
	if len(guardErr.RefChanges) != 2 || !names["refs/heads/"+currentBranch] || !names["refs/heads/bypassed"] {
		t.Fatalf("ref changes = %#v", guardErr.RefChanges)
	}
}
