package parentevidence

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDeletedNumericReviewEvidenceUsesDeletionDiff(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.Remove(filepath.Join(repoRoot, "review.go")); err != nil {
		t.Fatal(err)
	}
	openReviewCoverageBinding(t, repoRoot, st, "review.go:2")

	if err := PrintReviewEvidence(repoRoot, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil || binding.Proof == nil || len(binding.Proof.Claims) != 1 {
		t.Fatalf("binding proof = %#v err=%v", binding, err)
	}
	if binding.Proof.Claims[0].Kind != "diff" {
		t.Fatalf("deleted numeric proof kind = %q", binding.Proof.Claims[0].Kind)
	}
}
