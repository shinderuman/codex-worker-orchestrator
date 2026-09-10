package workflow

import (
	"os"
	"testing"
)

func TestRiskSurfaceDecisionsAllowMissingBaselineHead(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	if err := os.Remove(st.Path("baseline-head")); err != nil {
		t.Fatal(err)
	}

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	called := false
	w.collectChangedPaths = func(_ string, baselineHead string) ([]string, error) {
		called = true
		if baselineHead != "" {
			t.Fatalf("missing baseline head should use empty baseline, got %q", baselineHead)
		}
		return nil, nil
	}
	selfProtection, qualityEvidence := w.riskSurfaceDecisions()
	if !called {
		t.Fatal("changed-path classification was not reached for missing baseline head")
	}
	if selfProtection.High || qualityEvidence.High {
		t.Fatalf("missing baseline head should preserve unborn-baseline semantics: self=%#v quality=%#v", selfProtection, qualityEvidence)
	}
}

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
