package state

import "testing"

func TestInvalidateSessionRemovesOnlyRequestedRolePair(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	workerID, _, err := st.SessionID(WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(WorkerRole); err != nil {
		t.Fatal(err)
	}
	reviewerID, _, err := st.SessionID(ReviewerRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(ReviewerRole); err != nil {
		t.Fatal(err)
	}

	if err := st.InvalidateSession(WorkerRole); err != nil {
		t.Fatal(err)
	}
	if st.Exists("worker.id") || st.Exists("worker.ready") {
		t.Fatal("worker session state was not fully invalidated")
	}
	if !st.Exists("reviewer.id") || !st.Exists("reviewer.ready") {
		t.Fatal("reviewer session state was unexpectedly invalidated")
	}
	currentReviewerID, ready, err := st.SessionID(ReviewerRole)
	if err != nil {
		t.Fatal(err)
	}
	if currentReviewerID != reviewerID || !ready {
		t.Fatalf("reviewer session changed: id=%q ready=%v", currentReviewerID, ready)
	}
	currentWorkerID, ready, err := st.SessionID(WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	if currentWorkerID == workerID || ready {
		t.Fatalf("worker session was not rotated: old=%q new=%q ready=%v", workerID, currentWorkerID, ready)
	}
}

func TestInvalidateAllSessionsRemovesBothRolePairs(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	for _, role := range []SessionRole{WorkerRole, ReviewerRole} {
		if _, _, err := st.SessionID(role); err != nil {
			t.Fatal(err)
		}
		if err := st.MarkReady(role); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.InvalidateAllSessions(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"worker.id", "worker.ready", "reviewer.id", "reviewer.ready"} {
		if st.Exists(name) {
			t.Fatalf("session state %s remains after invalidation", name)
		}
	}
}
