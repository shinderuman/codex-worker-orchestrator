package autoresume

import (
	"testing"
	"time"
)

func TestBuildCodexWakeTransactionUsesUniqueNonce(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	first, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.TransactionID == second.TransactionID {
		t.Fatalf("identical wake plans reused token identity: first=%s second=%s", first.TransactionID, second.TransactionID)
	}
}
