package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func newLedgerScopeStore(t *testing.T) *StateStore {
	t.Helper()
	st, err := NewStateStore(config.AppConfig{
		StateBase: filepath.Join(t.TempDir(), "state"),
		RepoHash:  "ledgerscope",
		RepoRoot:  filepath.Join(t.TempDir(), "repo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	return st
}

func claimLedgerDigest(t *testing.T, st *StateStore, digest string) {
	t.Helper()
	if err := st.SaveParentEvidenceLedgerEntry(ParentEvidenceLedgerEntry{
		Surface: ParentEvidenceSurfaceSearch, Digest: digest, Origin: ParentEvidenceOriginStandalone,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestParentEvidenceLedgerRetainsEveryDigestWithinLease(t *testing.T) {
	st := newLedgerScopeStore(t)
	claimLedgerDigest(t, st, "digest-a")
	claimLedgerDigest(t, st, "digest-b")

	for _, digest := range []string{"digest-a", "digest-b"} {
		if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, digest); err != nil || !delivered {
			t.Fatalf("digest %s delivered=%v err=%v, want retained delivery", digest, delivered, err)
		}
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-c"); err != nil || delivered {
		t.Fatalf("undelivered digest reported delivered=%v err=%v", delivered, err)
	}
}

func TestStartNewTaskClearsParentEvidenceLedger(t *testing.T) {
	st := newLedgerScopeStore(t)
	claimLedgerDigest(t, st, "digest-a")

	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || delivered {
		t.Fatalf("previous task digest delivered=%v err=%v after StartNewTask", delivered, err)
	}
}

func TestParentEvidenceLeaseRotatesOnNewReviewLease(t *testing.T) {
	st := newLedgerScopeStore(t)
	claimLedgerDigest(t, st, "digest-a")

	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.WaitForSolReview(); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || delivered {
		t.Fatalf("old lease digest delivered=%v err=%v after review lease rotation", delivered, err)
	}

	claimLedgerDigest(t, st, "digest-a")
	if err := st.WaitForSolReview(); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || !delivered {
		t.Fatalf("same-lease re-entry lost digests: delivered=%v err=%v", delivered, err)
	}
}

func TestParentEvidenceLeaseRotatesOnDecisionLeaseStart(t *testing.T) {
	st := newLedgerScopeStore(t)
	claimLedgerDigest(t, st, "digest-a")

	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || delivered {
		t.Fatalf("old lease digest delivered=%v err=%v after decision lease rotation", delivered, err)
	}
}

func TestParkCycleKeepsParentEvidenceLeaseDigests(t *testing.T) {
	st := newLedgerScopeStore(t)
	claimLedgerDigest(t, st, "digest-a")

	record := ParkRecord{ParkID: "0123456789abcdef", TaskID: st.ReadOr("task.id", ""), RepoRoot: "repo", Head: "head", Worktree: "worktree", Branch: "branch"}
	if err := st.EnterParked(record); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || !delivered {
		t.Fatalf("parked lease digest delivered=%v err=%v", delivered, err)
	}
	if _, err := st.LeaveParked(); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || !delivered {
		t.Fatalf("same-lease digests after unpark delivered=%v err=%v", delivered, err)
	}
}

func TestParentEvidenceLedgerResetsOnUnsupportedVersion(t *testing.T) {
	st := newLedgerScopeStore(t)
	if err := os.WriteFile(st.Path("parent-evidence-ledger.json"), []byte(`{"version":1,"entries":{"search":[{"surface":"search","digest":"digest-a"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || delivered {
		t.Fatalf("unsupported ledger version delivered=%v err=%v, want strict reset", delivered, err)
	}
}
