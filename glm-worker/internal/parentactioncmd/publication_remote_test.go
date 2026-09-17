package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationRemoteWriteRequiresPromotedReadyCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("remote candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "remote publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")

	before := publicationRemoteWriteFixture(baseHead, false)
	blocked := applyPublicationRemoteWriteGuardForTask(cfg, st, before)
	if blocked.Status != "blocked" || blocked.RemoteWrite != nil || blocked.Failure == nil || blocked.Failure.Reason != publicationFailureRemoteNotReady {
		t.Fatalf("pre-promotion remote write = %#v", blocked)
	}

	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted {
		t.Fatalf("promotion = %#v", promoted)
	}
	after := publicationRemoteWriteFixture(candidate.CommitOID, true)
	allowed := applyPublicationRemoteWriteGuardForTask(cfg, st, after)
	if allowed.Status == "blocked" || allowed.Failure != nil || allowed.RemoteWrite == nil ||
		allowed.RemoteWrite.Authorization != pushBindingAuthorizationPublication || allowed.RemoteWrite.ExpectedOID != candidate.CommitOID {
		t.Fatalf("promoted remote write = %#v", allowed)
	}
}

func TestPublicationRemoteWriteRejectsActiveTaskWithoutCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	head := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")
	output := applyPublicationRemoteWriteGuardForTask(cfg, st, publicationRemoteWriteFixture(head, true))
	if output.Status != "blocked" || output.RemoteWrite != nil || output.Failure == nil || output.Failure.Reason != publicationFailureRemoteNotReady {
		t.Fatalf("candidate-less remote write = %#v", output)
	}
}

func publicationRemoteWriteFixture(oid string, clean bool) pushBindingOutput {
	return pushBindingOutput{
		Status:         "classified",
		ExpectedOID:    oid,
		TreeClean:      clean,
		Classification: pushBindingClassificationLocalAhead,
		Target:         &pushBindingTarget{LocalOID: oid, RemoteName: "origin", RemoteRef: "refs/heads/main"},
		RemoteWrite: &pushBindingRemoteWrite{
			Authorization: pushBindingAuthorizationStanding,
			Executor:      pushBindingExecutorParentOnly,
			RemoteName:    "origin",
			RemoteRef:     "refs/heads/main",
			ExpectedOID:   oid,
		},
	}
}
