package state

import "testing"

func TestResumeCheckpointGuardRefEvidenceRoundTrip(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{
		Stage:                    ResumeStageReview,
		Phase:                    "reviewer-1",
		Role:                     ReviewerRole,
		Model:                    "sonnet",
		ReadOnly:                 true,
		GuardRecoverable:         true,
		GuardFailure:             "git authority guard failed: after-call-mutation: refs",
		GuardRefBeforeDigest:     "before",
		GuardRefAfterDigest:      "after",
		GuardRefChangesTruncated: true,
		GuardRefChanges: []GuardRefChange{{
			Name:   "refs/heads/bypass",
			Before: nil,
			After:  &GuardRefState{Name: "refs/heads/bypass", ObjectID: "abc123"},
		}},
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if got.GuardRefBeforeDigest != "before" || got.GuardRefAfterDigest != "after" || !got.GuardRefChangesTruncated || len(got.GuardRefChanges) != 1 {
		t.Fatalf("guard ref evidence = %#v", got)
	}
	change := got.GuardRefChanges[0]
	if change.Name != "refs/heads/bypass" || change.Before != nil || change.After == nil || change.After.ObjectID != "abc123" {
		t.Fatalf("guard ref change = %#v", change)
	}
}
