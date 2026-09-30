//go:build !unix

package parentactioncmd

import (
	"strings"
	"testing"
	"time"
)

func TestRunGuardRepairCommandFailsClosedWithoutProcessTreeOwnership(t *testing.T) {
	_, err := runGuardRepairCommandWithin(t.TempDir(), "guard repair Go tests", time.Second, "unused")
	if err == nil || !strings.Contains(err.Error(), "process-tree ownership unavailable") {
		t.Fatalf("unsupported platform error = %v", err)
	}
}

func guardRepairProcessTreeSupportedForTest() bool {
	return false
}
