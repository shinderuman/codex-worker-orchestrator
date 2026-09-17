package parentactioncmd

import (
	"strings"
	"testing"
)

func TestUsageAdvertisesReviewEvidence(t *testing.T) {
	if !strings.Contains(usage, "| review-evidence |") {
		t.Fatalf("parent action usage omits review-evidence: %s", usage)
	}
}
