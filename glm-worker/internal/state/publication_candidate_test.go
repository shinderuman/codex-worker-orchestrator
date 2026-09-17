package state

import (
	"strings"
	"testing"
	"time"
)

func TestPublicationCandidatePersistsTaskBoundIdentity(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := SnapshotDigest{
		Head:           strings.Repeat("1", 40),
		IndexDigest:    strings.Repeat("2", 64),
		WorktreeDigest: strings.Repeat("3", 64),
	}
	candidate := PublicationCandidate{
		Version:    publicationCandidateVersion,
		TaskID:     taskID,
		BaseHead:   snapshot.Head,
		CommitOID:  strings.Repeat("4", 40),
		TreeOID:    strings.Repeat("5", 40),
		Snapshot:   snapshot,
		SnapshotID: ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt: time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != candidate.TaskID || got.CommitOID != candidate.CommitOID || got.TreeOID != candidate.TreeOID || got.SnapshotID != candidate.SnapshotID {
		t.Fatalf("publication candidate = %#v", got)
	}
}

func TestPublicationCandidateRejectsSnapshotMismatch(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	candidate := PublicationCandidate{
		Version:    publicationCandidateVersion,
		TaskID:     taskID,
		BaseHead:   strings.Repeat("1", 40),
		CommitOID:  strings.Repeat("4", 40),
		TreeOID:    strings.Repeat("5", 40),
		Snapshot:   SnapshotDigest{Head: strings.Repeat("1", 40), IndexDigest: strings.Repeat("2", 64), WorktreeDigest: strings.Repeat("3", 64)},
		SnapshotID: strings.Repeat("f", 64),
		PreparedAt: time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err == nil || !strings.Contains(err.Error(), "snapshot ID") {
		t.Fatalf("snapshot mismatch was accepted: %v", err)
	}
}

func TestPublicationCandidateRejectsOtherTask(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	otherTaskID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := SnapshotDigest{Head: strings.Repeat("1", 40), IndexDigest: strings.Repeat("2", 64), WorktreeDigest: strings.Repeat("3", 64)}
	candidate := PublicationCandidate{
		Version:    publicationCandidateVersion,
		TaskID:     otherTaskID,
		BaseHead:   snapshot.Head,
		CommitOID:  strings.Repeat("4", 40),
		TreeOID:    strings.Repeat("5", 40),
		Snapshot:   snapshot,
		SnapshotID: ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt: time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err == nil || !strings.Contains(err.Error(), "does not match current task") {
		t.Fatalf("other task candidate was accepted: %v", err)
	}
}
