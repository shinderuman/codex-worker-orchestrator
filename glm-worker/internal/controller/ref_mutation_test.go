package controller

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUnrelatedRepositoryRefMutationKeepsLeaseAuthority(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	before, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := controllerTestTask(t, repo)
	admission, err := store.BootstrapExecution(task, workspace, before)
	if err != nil {
		t.Fatal(err)
	}

	runControllerGit(t, repo, "branch", "controller-ref")
	runControllerGit(t, repo, "update-ref", "refs/remotes/origin/controller-ref", before.Head)
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("unrelated ref mutation changed the lease-bound snapshot: before=%#v after=%#v", before, after)
	}
	if _, err := store.AdmitMutation(admission.MutationAuthority(), workspace, after); err != nil {
		t.Fatalf("unrelated ref mutation was rejected by the live lease: %v", err)
	}
}

func TestAuthorityRefMutationInvalidatesLeaseAndRecordsRefProvenance(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	before, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := controllerTestTask(t, repo)
	admission, err := store.BootstrapExecution(task, workspace, before)
	if err != nil {
		t.Fatal(err)
	}

	currentBranch := strings.TrimSpace(controllerGitOutput(t, repo, "symbolic-ref", "--short", "HEAD"))
	runControllerGit(t, repo, "branch", "-m", currentBranch+"-moved")
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.Head != before.Head {
		t.Fatalf("authority branch rename unexpectedly moved HEAD: before=%s after=%s", before.Head, after.Head)
	}
	if after.RefDigest == before.RefDigest || after.ID == before.ID {
		t.Fatalf("authority branch rename did not change snapshot identity: before=%#v after=%#v", before, after)
	}
	if _, err := store.AdmitMutation(admission.MutationAuthority(), workspace, after); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("authority-ref-mutated workspace retained stale lease authority: %v", err)
	}

	advanced, err := store.RecordAdmittedMutation(admission, "test-ref-mutation", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Snapshot.RefDigest != after.RefDigest {
		t.Fatalf("advanced lease did not bind new ref snapshot: %#v", advanced.Snapshot)
	}
	paths, err := filepath.Glob(filepath.Join(store.dir, "mutations", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("mutation provenance files = %v err=%v", paths, err)
	}
	var record MutationRecord
	if err := readJSON(paths[0], &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Surfaces) != 1 || record.Surfaces[0] != MutationSurfaceRef {
		t.Fatalf("authority-ref mutation surfaces = %v", record.Surfaces)
	}
}
