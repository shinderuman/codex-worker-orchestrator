package workflow

import (
	"os"
	"testing"
)

func TestRiskSurfaceDecisionsFailClosedWhenBaselineHeadUnreadable(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	if err := os.Remove(st.Path("baseline-head")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.Path("baseline-head"), 0o700); err != nil {
		t.Fatal(err)
	}

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	selfProtection, qualityEvidence := w.riskSurfaceDecisions()
	if !selfProtection.High || selfProtection.Source != "classify-error" || selfProtection.HitPath == "" {
		t.Fatalf("baseline read failure did not fail closed: %#v", selfProtection)
	}
	if qualityEvidence.High {
		t.Fatalf("baseline read failure should be represented by one high-risk source: %#v", qualityEvidence)
	}
}
