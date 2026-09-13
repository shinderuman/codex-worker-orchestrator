package state

import "testing"

func TestGuardRepairIntegratingTransitionRequiresExplicitExit(t *testing.T) {
	st := newGuardRepairStateStore(t)
	parents := ParentFileStates{}
	record := guardRepairRecordForTest()
	record.Status = GuardRepairIntegrating
	record.Integration = &GuardRepairIntegration{
		RepositoryBoundary: GitSnapshot{
			Head:           "head",
			IndexDigest:    "index",
			WorktreeDigest: "worktree",
			ParentFiles:    &parents,
		},
		Files: []GuardRepairIntegrationFile{{Path: "guard.go"}},
	}
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}

	for name, nextStatus := range map[string]GuardRepairStatus{
		"failed":    GuardRepairFailed,
		"requested": GuardRepairRequested,
	} {
		t.Run(name, func(t *testing.T) {
			next := record
			next.Status = nextStatus
			next.Integration = nil
			if err := st.SaveGuardRepairRecord(next); err == nil {
				t.Fatalf("ordinary %s transition discarded in-flight rollback authority", nextStatus)
			}
		})
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GuardRepairIntegrating || got.Integration == nil {
		t.Fatalf("rejected transition changed integration transaction: %#v", got)
	}

	rolledBack := record
	rolledBack.Status = GuardRepairRequested
	rolledBack.Integration = nil
	if err := st.RollbackGuardRepairIntegration(rolledBack); err != nil {
		t.Fatalf("explicit rollback transition rejected: %v", err)
	}
}

func TestGuardRepairIntegratingCommitRequiresMatchingTransaction(t *testing.T) {
	st := newGuardRepairStateStore(t)
	parents := ParentFileStates{}
	record := guardRepairRecordForTest()
	record.Status = GuardRepairIntegrating
	record.Integration = &GuardRepairIntegration{
		RepositoryBoundary: GitSnapshot{
			Head:           "head",
			IndexDigest:    "index",
			WorktreeDigest: "worktree",
			ParentFiles:    &parents,
		},
		Files: []GuardRepairIntegrationFile{{Path: "guard.go"}},
	}
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}

	ready := record
	ready.Status = GuardRepairReady
	ready.RepairedDigest = "digest-repaired"
	ready.Integration = nil
	foreign := ready
	foreign.Fingerprint = "other-fingerprint"
	if err := st.CommitGuardRepairIntegration(foreign); err == nil {
		t.Fatal("foreign transaction committed guard repair integration")
	}
	if err := st.CommitGuardRepairIntegration(ready); err != nil {
		t.Fatalf("matching integration commit rejected: %v", err)
	}
}
