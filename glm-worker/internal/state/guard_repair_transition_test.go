package state

import "testing"

func TestGuardRepairIntegratingTransitionRetainsRollbackAuthority(t *testing.T) {
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

	failed := record
	failed.Status = GuardRepairFailed
	failed.Integration = nil
	if err := st.SaveGuardRepairRecord(failed); err == nil {
		t.Fatal("failed transition discarded in-flight rollback authority")
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
	if err := st.SaveGuardRepairRecord(rolledBack); err != nil {
		t.Fatalf("explicit rollback transition rejected: %v", err)
	}
}
