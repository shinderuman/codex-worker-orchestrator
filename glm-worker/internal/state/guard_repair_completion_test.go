package state

import "testing"

func TestGuardRepairCompleteRejectsMissingOriginalResumeEvidence(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	record.Status = GuardRepairComplete
	record.RepairedDigest = "digest-repaired"

	if err := st.SaveGuardRepairRecord(record); err == nil {
		t.Fatal("complete guard repair without original resume evidence was accepted")
	}

	record.OriginalResumeObserved = true
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
}
