package reviewtarget

import (
	"errors"
	"testing"
)

func TestTargetDiffFailureUsesTypedProofError(t *testing.T) {
	_, err := targetDiff(t.TempDir()+"/missing-repository", "a.go")
	if err == nil {
		t.Fatal("missing repository unexpectedly produced diff evidence")
	}
	var proofErr *ProofError
	if !errors.As(err, &proofErr) {
		t.Fatalf("diff failure type = %T want *ProofError", err)
	}
	if proofErr.Code != "diff-unavailable" || proofErr.Cause == nil {
		t.Fatalf("diff proof error = %+v", proofErr)
	}
}
