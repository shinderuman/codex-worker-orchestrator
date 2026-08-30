package runner

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
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
	guarded, _ := newGitAuthorityProductionRunner(t, root, "\"$REAL_GIT\" branch bypass", map[string]string{"REAL_GIT": realGit})
	_, err = guarded.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "after-call-mutation" {
		t.Fatalf("guard error = %#v", err)
	}
	if guardErr.RefBeforeDigest != beforeDigest || guardErr.RefAfterDigest == "" || guardErr.RefAfterDigest == beforeDigest {
		t.Fatalf("ref digests = before:%q after:%q want before:%q", guardErr.RefBeforeDigest, guardErr.RefAfterDigest, beforeDigest)
	}
	if guardErr.RefChangesTruncated {
		t.Fatal("single ref change must not be truncated")
	}
	if len(guardErr.RefChanges) != 1 {
		t.Fatalf("ref changes = %#v", guardErr.RefChanges)
	}
	change := guardErr.RefChanges[0]
	if change.Name != "refs/heads/bypass" || change.Before != nil || change.After == nil || change.After.ObjectID == "" {
		t.Fatalf("unexpected ref change = %#v", change)
	}
}
