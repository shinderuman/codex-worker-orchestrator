package controller

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStateStoreRewriteCannotMintExecutionAuthority(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	cfg := controllerTestConfig(repo, stateRoot)
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	committed := controllerTestTask(t, repo)
	admission, err := store.BootstrapExecution(committed, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Write("active-task", "IMPLEMENTATION_TASKS/forged.md"); err != nil {
		t.Fatal(err)
	}
	forged := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/forged.md", ContractDigest: strings.Repeat("f", 64)}
	claim := admission.MutationAuthority()
	claim.SemanticTaskRef = forged
	if _, err := store.AdmitMutation(claim, workspace, snapshot); err == nil {
		t.Fatal("StateStore rewrite minted controller authority")
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != before.ControllerGeneration || after.LiveAttemptID != before.LiveAttemptID || after.LiveLeaseID != before.LiveLeaseID {
		t.Fatalf("rejected StateStore rewrite changed live controller authority: before=%#v after=%#v", before, after)
	}
	if after.ExecutionTaskRef == nil || !after.ExecutionTaskRef.Equal(committed) {
		t.Fatalf("rejected StateStore rewrite changed execution task authority: %#v", after)
	}
}
