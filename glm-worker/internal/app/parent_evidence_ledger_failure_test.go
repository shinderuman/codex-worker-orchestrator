package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParentEvidenceLedgerLockFailureFailsClosed(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	if err := os.Mkdir(fixture.st.Path(parentEvidenceLedgerLockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := printStatusLeased(fixture.st, &out)
	if err == nil || !strings.Contains(err.Error(), "parent evidence ledger lock") {
		t.Fatalf("lock failure = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("lock failure projected evidence: %s", out.String())
	}
}
