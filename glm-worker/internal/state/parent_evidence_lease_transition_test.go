package state

import "testing"

func TestParentEvidenceLeaseTransitionsRollbackAfterAdvanceFailure(t *testing.T) {
	tests := []struct {
		name       string
		failState  string
		transition func(*StateStore) error
	}{
		{
			name:      "wait for decision pending state",
			failState: "pending-decision",
			transition: func(st *StateStore) error {
				return st.WaitForDecision()
			},
		},
		{
			name:      "wait for decision status",
			failState: "task.status",
			transition: func(st *StateStore) error {
				return st.WaitForDecision()
			},
		},
		{
			name:      "finish review status",
			failState: "task.status",
			transition: func(st *StateStore) error {
				return st.FinishReview(TaskStatusWaitingSolReview)
			},
		},
		{
			name:      "wait for sol review status",
			failState: "task.status",
			transition: func(st *StateStore) error {
				return st.WaitForSolReview()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLedgerScopeStore(t)
			if err := st.SetTaskStatus(TaskStatusActive); err != nil {
				t.Fatal(err)
			}
			claimLedgerDigest(t, st, "digest-a")
			beforeLease, err := st.ParentEvidenceLeaseEpoch()
			if err != nil {
				t.Fatal(err)
			}
			failWritesFor(t, st, test.failState)

			if err := test.transition(st); err == nil {
				t.Fatal("injected lifecycle write failure must fail the transition")
			}
			if st.TaskStatus() != TaskStatusActive {
				t.Fatalf("failed transition status = %s want active", st.TaskStatus())
			}
			afterLease, err := st.ParentEvidenceLeaseEpoch()
			if err != nil {
				t.Fatal(err)
			}
			if afterLease != beforeLease {
				t.Fatalf("failed transition lease = %d want %d", afterLease, beforeLease)
			}
			if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || !delivered {
				t.Fatalf("failed transition lost old delivery authority: delivered=%v err=%v", delivered, err)
			}
			if st.Exists("pending-decision") {
				t.Fatal("failed transition left a pending decision marker")
			}
		})
	}
}

func TestFinishReviewWaitingSolReviewAdvancesParentEvidenceLease(t *testing.T) {
	st := newLedgerScopeStore(t)
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	claimLedgerDigest(t, st, "digest-a")
	beforeLease, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}

	if err := st.FinishReview(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	afterLease, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if afterLease != beforeLease+1 {
		t.Fatalf("successful transition lease = %d want %d", afterLease, beforeLease+1)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(ParentEvidenceSurfaceSearch, "digest-a"); err != nil || delivered {
		t.Fatalf("successful transition retained old delivery authority: delivered=%v err=%v", delivered, err)
	}
}
