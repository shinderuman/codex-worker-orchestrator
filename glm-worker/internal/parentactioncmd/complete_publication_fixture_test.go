package parentactioncmd

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func ensureCompleteFixturePublicationAuthority(t *testing.T, fixture *completeFixture) {
	t.Helper()
	activation, err := fixture.st.Read(repositoryharness.ActivationStateKey)
	if err != nil || activation != repositoryharness.ActivationActiveValue {
		return
	}
	if _, err := fixture.st.LoadPublicationCandidate(); err == nil {
		return
	}
	if _, _, err := runtimeInstallRequirementForTask(fixture.repo, fixture.st); err != nil {
		if !strings.Contains(err.Error(), "baseline is unavailable") {
			t.Fatalf("completion fixture runtime baseline: %v", err)
		}
		if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
			t.Fatalf("completion fixture baseline capture: %v", err)
		}
	}

	head := strings.TrimSpace(pushBindingGitOutput(t, fixture.repo, "rev-parse", "HEAD"))
	parent := strings.TrimSpace(pushBindingGitOutput(t, fixture.repo, "rev-parse", "HEAD^"))
	tree := strings.TrimSpace(pushBindingGitOutput(t, fixture.repo, "rev-parse", "HEAD^{tree}"))
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.SnapshotDigest{
		Head:                          parent,
		IndexDigest:                   completionFixtureDigest("index", head),
		WorktreeDigest:                completionFixtureDigest("worktree", head),
		WorktreeDigestExcludingParent: completionFixtureDigest("worktree-excluding-parent", head),
	}
	candidate := state.PublicationCandidate{
		Version:       1,
		TaskID:        taskID,
		BaseHead:      parent,
		CommitOID:     head,
		TreeOID:       tree,
		MessageDigest: completionFixtureDigest("message", head),
		Snapshot:      snapshot,
		SnapshotID:    state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt:    time.Now().UTC(),
	}
	if err := fixture.st.SavePublicationCandidate(candidate); err != nil {
		t.Fatalf("completion fixture publication candidate: %v", err)
	}
}

func markCompleteFixtureNonHarness(t *testing.T, fixture *completeFixture) {
	t.Helper()
	if err := fixture.st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationInactiveValue); err != nil {
		t.Fatal(err)
	}
}

func completionFixtureDigest(kind, head string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + head))
	return hex.EncodeToString(digest[:])
}
