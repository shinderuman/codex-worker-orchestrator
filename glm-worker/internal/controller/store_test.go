package controller

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestControllerExistenceProbeDoesNotActivateStore(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	cfg := controllerTestConfig(repo, stateRoot)
	exists, err := Exists(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("controller exists before activation")
	}
	identity, err := ResolveRepositoryIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	base := controllerStoreDir(cfg, identity)
	if _, err := os.Stat(base); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existence probe created controller state: %v", err)
	}
	if _, err := Open(cfg); err != nil {
		t.Fatal(err)
	}
	exists, err = Exists(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("activated controller was not detected")
	}
}

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
	primaryLock := filepath.Join(primary.dir, "lock")
	secondaryLock := filepath.Join(secondary.dir, "lock")
	if primaryLock != secondaryLock {
		t.Fatalf("linked worktrees have different controller locks: %s != %s", primaryLock, secondaryLock)
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
	if first.dir == second.dir {
		t.Fatal("separate clones unexpectedly share repository controller store")
	}
}

func TestExecutionLeaseRejectsLinkedWorkspaceAndStaleSnapshot(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	store, err := Open(controllerTestConfig(repo, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	task := controllerTestTask(t, repo)
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
	if _, err := store.AdmitMutation(admission.MutationAuthority(), linkedWorkspace, linkedSnapshot); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("linked workspace obtained live mutation authority: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitMutation(admission.MutationAuthority(), primaryWorkspace, changed); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("stale lease admitted changed workspace snapshot: %v", err)
	}
	advanced, err := store.RecordAdmittedMutation(admission, "test-mutation", "success", changed)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Head.ControllerGeneration <= admission.Head.ControllerGeneration || advanced.Lease.LeaseID == admission.Lease.LeaseID {
		t.Fatalf("mutation did not advance lease authority: before=%#v after=%#v", admission.Lease, advanced.Lease)
	}
	if _, err := store.RecordAdmittedMutation(admission, "stale-reuse", "success", changed); err == nil {
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
	task := controllerTestTask(t, repo)
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

func TestMutablePlanCannotMintExecutionAuthority(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	store, err := Open(controllerTestConfig(repo, stateRoot))
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

	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/forged.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "forged.md"), []byte("# forged\n\n## Contract\n\nforged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	forgedAuthority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !forgedAuthority.Task.Equal(committed) {
		t.Fatalf("uncommitted Plan rewrite changed committed task authority: got=%#v want=%#v", forgedAuthority.Task, committed)
	}
	forged := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/forged.md", ContractDigest: strings.Repeat("f", 64)}
	changed, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	claim := admission.MutationAuthority()
	claim.SemanticTaskRef = forged
	if _, err := store.AdmitMutation(claim, workspace, changed); err == nil {
		t.Fatal("uncommitted Plan/Task rewrite minted execution authority")
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
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootTask := "# root\n\n## Contract\n\ncontroller root task\n\n## Dependencies\n\nnone\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte(rootTask), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "base")
	runControllerGit(t, repo, "worktree", "add", "-q", "--detach", linked, "HEAD")
	return repo, linked
}

func controllerTestTask(t *testing.T, repo string) SemanticTaskRef {
	t.Helper()
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	return authority.Task
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
