package state

import "testing"

func TestGuardRepairCompleteRejectsMissingOriginalResumeEvidence(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	record.Status = GuardRepairComplete
	record.RepairedDigest = "digest-repaired"
	record.ResumeAttemptID = "11111111-1111-4111-8111-111111111111"
	record.ResumeCheckpointDigest = "checkpoint-digest"

	if err := st.SaveGuardRepairRecord(record); err == nil {
		t.Fatal("complete guard repair without original resume evidence was accepted")
	}

	record.OriginalResumeObserved = true
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
}
