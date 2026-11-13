package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSourceRecoveryRejectsArchiveForDifferentCommit(t *testing.T) {
	t.Parallel()
	store, project := syntheticProjectSourceFixture(t)
	repo := store.Identity().PrimaryRoot
	head := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	wrong, _, err := store.CaptureGitObjectArchive(repo, "wrong-project-source", []string{head})
	if err != nil {
		t.Fatal(err)
	}
	project.SourceArchive = &wrong
	writeProjectSourceTestSnapshot(t, store, project)
	pruneProjectSource(t, store, project.HeadOID)
	if _, err := store.LoadProjectSnapshot(project.SnapshotID); err == nil || !strings.Contains(err.Error(), "archive root differs") {
		t.Fatalf("project accepted an archive for another commit: %v", err)
	}
}

func syntheticProjectSourceFixture(t *testing.T) (*Store, ProjectSnapshot) {
	t.Helper()
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
	return store, authority.Snapshot
}

func writeProjectSourceTestSnapshot(t *testing.T, store *Store, project ProjectSnapshot) {
	t.Helper()
	if err := writeJSONAtomic(filepath.Join(store.dir, "project-snapshots", project.SnapshotID+".json"), project); err != nil {
		t.Fatal(err)
	}
}

func pruneProjectSource(t *testing.T, store *Store, oid string) {
	t.Helper()
	repo := store.Identity().PrimaryRoot
	runControllerGit(t, repo, "reflog", "expire", "--expire=now", "--all")
	runControllerGit(t, repo, "gc", "--prune=now")
	if _, err := runGitBinary(repo, nil, "cat-file", "-e", oid); err == nil {
		t.Fatal("synthetic project source remained reachable")
	}
}

func TestProjectSourcePresentReadsDoNotConsumeArchive(t *testing.T) {
	t.Parallel()
	store, project := syntheticProjectSourceFixture(t)
	if err := os.Remove(store.evidenceObjectPath(project.SourceArchive.Digest)); err != nil {
		t.Fatal(err)
	}
	for read := 0; read < 3; read++ {
		opened, err := Open(store.config)
		if err != nil {
			t.Fatal(err)
		}
		got, err := opened.LoadProjectSnapshot(project.SnapshotID)
		if err != nil || got.HeadOID != project.HeadOID {
			t.Fatalf("normal snapshot read %d consumed unavailable archive: %v", read, err)
		}
		if len(opened.archiveVerified) != 0 {
			t.Fatal("normal snapshot read verified an archive pack")
		}
	}
	if _, err := store.VerifyGitObjectArchive(*project.SourceArchive); err == nil {
		t.Fatal("explicit audit accepted the missing archive")
	}
}

func TestProjectSourcePresentReadRejectsInvalidArchiveType(t *testing.T) {
	t.Parallel()
	for _, claim := range []string{"kind", "media-type"} {
		t.Run(claim, func(t *testing.T) {
			store, project := syntheticProjectSourceFixture(t)
			if claim == "kind" {
				project.SourceArchive.Kind = "runtime-artifact"
			} else {
				project.SourceArchive.MediaType = "application/json"
			}
			writeProjectSourceTestSnapshot(t, store, project)
			if _, err := store.LoadProjectSnapshot(project.SnapshotID); err == nil {
				t.Fatal("normal snapshot read accepted an invalid archive reference type")
			}
		})
	}
}

func TestProjectSourceRecoveryRejectsCorruptArchive(t *testing.T) {
	t.Parallel()
	store, project := syntheticProjectSourceFixture(t)
	if err := os.WriteFile(store.evidenceObjectPath(project.SourceArchive.Digest), []byte("corrupt archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruneProjectSource(t, store, project.HeadOID)
	_, err := store.LoadProjectSnapshot(project.SnapshotID)
	var integrity *EvidenceIntegrityError
	if !errors.As(err, &integrity) {
		t.Fatalf("recovery did not reject corrupt archive evidence: %v", err)
	}
}
