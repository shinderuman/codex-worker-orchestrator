package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestLinkedWorktreesShareControllerDomain(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	primaryCfg := controllerTestConfig(repo, stateRoot)
	linkedCfg := controllerTestConfig(linked, stateRoot)

	primary, err := Open(primaryCfg)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := Open(linkedCfg)
	if err != nil {
		t.Fatal(err)
	}
	if primary.Identity().LineageID != secondary.Identity().LineageID {
		t.Fatalf("linked worktrees have different controller identities: %s != %s", primary.Identity().LineageID, secondary.Identity().LineageID)
	}
	if primary.LockPath() != secondary.LockPath() {
		t.Fatalf("linked worktrees have different controller locks: %s != %s", primary.LockPath(), secondary.LockPath())
	}
}

func TestSeparateClonesUseSeparateControllerDomains(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runControllerGit(t, "", "clone", "-q", repo, clone)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")

	first, err := Open(controllerTestConfig(repo, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(controllerTestConfig(clone, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity().LineageID == second.Identity().LineageID {
		t.Fatal("separate clones unexpectedly share repository controller identity")
	}
	if first.LockPath() == second.LockPath() {
		t.Fatal("separate clones unexpectedly share repository controller lock")
	}
}

func TestExecutionLeaseRejectsLinkedWorkspaceAndStaleSnapshot(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	store, err := Open(controllerTestConfig(repo, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/root.md", ContractDigest: strings.Repeat("a", 64)}
	primaryWorkspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	primarySnapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := store.BootstrapExecution(task, primaryWorkspace, primarySnapshot)
	if err != nil {
		t.Fatal(err)
	}

	linkedWorkspace, err := ResolveWorkspaceIdentity(linked, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	linkedSnapshot, err := CaptureWorkspaceSnapshot(linked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitMutation(task, linkedWorkspace, linkedSnapshot); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("linked workspace obtained live mutation authority: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitMutation(task, primaryWorkspace, changed); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("stale lease admitted changed workspace snapshot: %v", err)
	}
	advanced, err := store.RecordMutation(admission, "test-mutation", "success", changed)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Head.ControllerGeneration <= admission.Head.ControllerGeneration || advanced.Lease.LeaseID == admission.Lease.LeaseID {
		t.Fatalf("mutation did not advance lease authority: before=%#v after=%#v", admission.Lease, advanced.Lease)
	}
	if _, err := store.RecordMutation(admission, "stale-reuse", "success", changed); err == nil {
		t.Fatal("stale lease was reused after controller generation advanced")
	}
}

func TestOnlyPrimaryWorktreeCanBootstrapMutationAuthority(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	store, err := Open(controllerTestConfig(linked, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(linked, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(linked)
	if err != nil {
		t.Fatal(err)
	}
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/rewritten.md", ContractDigest: strings.Repeat("b", 64)}
	if _, err := store.BootstrapExecution(task, workspace, snapshot); err == nil || !strings.Contains(err.Error(), "primary worktree") {
		t.Fatalf("derived worktree bootstrapped mutation authority: %v", err)
	}

	primaryStore, err := Open(controllerTestConfig(repo, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	head, err := primaryStore.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" {
		t.Fatalf("rejected derived bootstrap mutated controller head: %#v", head)
	}
}

func TestTransitionClassificationUsesExactOldAndNewState(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/main", ExpectedOld: "old", ExpectedNew: "new"}
	record, err := store.BeginTransition("TEST_REF_CAS", head.ControllerGeneration, []EffectExpectation{effect})
	if err != nil {
		t.Fatal(err)
	}
	for actual, want := range map[string]EffectClassification{
		"old":   EffectExpectedOld,
		"new":   EffectExpectedNew,
		"other": EffectUnexpected,
	} {
		got := store.ClassifyTransition(record, map[string]string{effect.Key(): actual})[effect.Key()]
		if got != want {
			t.Fatalf("classify actual=%q got=%s want=%s", actual, got, want)
		}
	}
	if err := store.MarkTransitionApplied(record, map[string]string{effect.Key(): "new"}); err != nil {
		t.Fatal(err)
	}
	loaded, stateRecord, err := store.LoadTransition(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if stateRecord.Phase != TransitionPhaseApplied {
		t.Fatalf("transition phase = %s", stateRecord.Phase)
	}
	if got := store.ClassifyTransition(loaded, map[string]string{effect.Key(): "old"})[effect.Key()]; got != EffectExpectedOld {
		t.Fatalf("classification depended on progress bit: %s", got)
	}
	committed, err := store.CommitTransition(record, map[string]string{effect.Key(): "new"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed.PendingTransitionID != "" || committed.ControllerGeneration != record.TargetGeneration {
		t.Fatalf("committed head = %#v", committed)
	}
}

func TestUnexpectedTransitionFailsClosedAndRevokesLease(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
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
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/root.md", ContractDigest: strings.Repeat("c", 64)}
	admission, err := store.BootstrapExecution(task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/main", ExpectedOld: "old", ExpectedNew: "new"}
	record, err := store.BeginTransition("TEST_UNEXPECTED", admission.Head.ControllerGeneration, []EffectExpectation{effect})
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{effect.Key(): "foreign"}
	if _, err := store.CommitTransition(record, actual, true, nil); err == nil {
		t.Fatal("unexpected transition state committed")
	}
	failure, err := store.FailClosed("unexpected ref state", record.TransitionID, workspace, snapshot, snapshot, actual)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != ControllerStatusFailClosed || head.LiveLeaseID != "" || head.FailureID != failure.FailureID {
		t.Fatalf("fail-closed head = %#v failure=%#v", head, failure)
	}
	if _, err := store.AdmitMutation(task, workspace, snapshot); err == nil {
		t.Fatal("fail-closed controller still admitted mutation")
	}
}

func newControllerLinkedWorktree(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	linked := filepath.Join(root, "linked")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "init", "-q")
	runControllerGit(t, repo, "config", "user.email", "controller@example.invalid")
	runControllerGit(t, repo, "config", "user.name", "Controller Test")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "source.txt")
	runControllerGit(t, repo, "commit", "-q", "-m", "base")
	runControllerGit(t, repo, "worktree", "add", "-q", "--detach", linked, "HEAD")
	return repo, linked
}

func controllerTestConfig(repoRoot, stateBase string) config.AppConfig {
	hash := config.RepoHashFor(repoRoot)
	return config.AppConfig{RepoRoot: repoRoot, RepoHash: hash, RepoShort: hash[:12], StateBase: stateBase}
}

func runControllerGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	commandArgs := args
	if dir != "" {
		commandArgs = append([]string{"-C", dir}, args...)
	}
	command := exec.Command("git", commandArgs...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
}
