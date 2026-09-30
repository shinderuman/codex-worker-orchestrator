package controller

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRefMutationInvalidatesLeaseAndRecordsRefProvenance(t *testing.T) {
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
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/root.md", ContractDigest: strings.Repeat("d", 64)}
	admission, err := store.BootstrapExecution(task, workspace, before)
	if err != nil {
		t.Fatal(err)
	}

	runControllerGit(t, repo, "branch", "controller-ref")
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.Head != before.Head {
		t.Fatalf("ref-only mutation unexpectedly moved HEAD: before=%s after=%s", before.Head, after.Head)
	}
	if after.RefDigest == before.RefDigest || after.ID == before.ID {
		t.Fatalf("ref-only mutation did not change snapshot identity: before=%#v after=%#v", before, after)
	}
	if _, err := store.AdmitMutation(task, workspace, after); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("ref-mutated workspace retained stale lease authority: %v", err)
	}

	advanced, err := store.RecordMutation(admission, "test-ref-mutation", "success", after)
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
		t.Fatalf("ref-only mutation surfaces = %v", record.Surfaces)
	}
}
