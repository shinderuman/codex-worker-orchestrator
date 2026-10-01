package controller

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvidenceObjectRoundTripAndCorruptionFailsLoudly(t *testing.T) {
	store := newEvidenceTestStore(t)
	ref, err := store.PutEvidenceObject("telemetry", "application/octet-stream", "attempt-a:telemetry", true, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("unexpected evidence payload: %q", data)
	}
	if err := os.WriteFile(store.evidenceObjectPath(ref.Digest), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadEvidenceObject(ref); err == nil {
		t.Fatal("corrupt evidence object was accepted")
	} else {
		var integrity *EvidenceIntegrityError
		if !errors.As(err, &integrity) {
			t.Fatalf("corruption did not produce integrity failure: %v", err)
		}
	}
}

func TestAttemptSealCanonicalizesEvidenceOrderAndSurvivesMissingRuntimePath(t *testing.T) {
	store := newEvidenceTestStore(t)
	archive, err := store.PutEvidenceObject("git-object-archive", "application/x-git-packed-objects", "attempt-a:git", true, []byte("portable-git"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PutEvidenceObject("telemetry", "application/jsonl", "attempt-a:telemetry", true, []byte("one\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutEvidenceObject("validation", "application/json", "attempt-a:validation", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1234, 567).UTC()
	record := AttemptSeal{
		SchemaVersion:         evidenceSchemaVersion,
		RepositoryIdentity:    store.Identity().LineageID,
		SemanticTaskRef:       SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/A.md", ContractDigest: "task-a"},
		RootTaskRef:           SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root"},
		AttemptID:             "attempt-a",
		EpisodeID:             "episode-a",
		EpisodeRevision:       2,
		ControllerGeneration:  7,
		SealingTransitionID:   "transition-a",
		RevokedLeaseID:        "lease-a",
		WorkspaceID:           "workspace-a",
		ExecutionPurpose:      "blocker-execution",
		StartedAt:             now.Add(-time.Minute),
		SealedAt:              now,
		Disposition:           "suspended-for-blocker",
		ExecutionBaseOID:      "base",
		BaselineIndexTree:     "baseline-index",
		BaselineWorktreeTree:  "baseline-worktree",
		CurrentIndexTree:      "current-index",
		CurrentWorktreeTree:   "current-worktree",
		ParentAuthorityDigest: "parent-authority",
		GitObjectArchive:      archive,
		EvidenceRefs:          []EvidenceObjectRef{second, first},
		Coverage:              "complete",
		Missing:               []string{"z", "a"},
		Unreadable:            []string{"y", "b"},
	}
	ref, stored, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	record.EvidenceRefs = []EvidenceObjectRef{first, second}
	record.Missing = []string{"a", "z"}
	record.Unreadable = []string{"b", "y"}
	refAgain, storedAgain, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Digest != refAgain.Digest || stored.AttemptSealID != storedAgain.AttemptSealID {
		t.Fatalf("canonical seal identity changed with input ordering: first=%s second=%s", stored.AttemptSealID, storedAgain.AttemptSealID)
	}
	loaded, err := store.LoadAttemptSeal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AttemptSealID != stored.AttemptSealID || loaded.WorkspaceID != "workspace-a" {
		t.Fatalf("attempt seal round-trip changed identity: %#v", loaded)
	}
}

func TestEvidenceMissingObjectFailsLoudly(t *testing.T) {
	store := newEvidenceTestStore(t)
	ref, err := store.PutEvidenceObject("review", "text/plain", "attempt-a:review", true, []byte("review"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.evidenceObjectPath(ref.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadEvidenceObject(ref); err == nil {
		t.Fatal("missing required evidence object was accepted")
	} else {
		var integrity *EvidenceIntegrityError
		if !errors.As(err, &integrity) {
			t.Fatalf("missing evidence did not produce integrity failure: %v", err)
		}
	}
}

func newEvidenceTestStore(t *testing.T) *Store {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	store, err := Open(controllerTestConfig(repo, stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
