package controller

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSourceRecoveryRejectsArchiveForDifferentCommit(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	store, repo := fixture.store, fixture.store.Identity().PrimaryRoot
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	_, authority, err := store.captureParentSourceRevision(snapshot.ID, snapshot.Head)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Snapshot.SourceArchive == nil {
		t.Fatal("synthetic project has no source archive")
	}
	if err := store.writeProjectSnapshot(authority.Snapshot); err != nil {
		t.Fatal(err)
	}
	wrong, _, err := store.CaptureGitObjectArchive(repo, "wrong-project-source", []string{snapshot.Head})
	if err != nil {
		t.Fatal(err)
	}
	project := authority.Snapshot
	project.SourceArchive = &wrong
	if err := writeJSONAtomic(filepath.Join(store.dir, "project-snapshots", project.SnapshotID+".json"), project); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadProjectSnapshot(project.SnapshotID); err == nil || !strings.Contains(err.Error(), "archive root differs") {
		t.Fatalf("project accepted an archive for another commit: %v", err)
	}
}
