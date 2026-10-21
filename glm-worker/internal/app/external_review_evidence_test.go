package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentEvidenceLedgerLockFailureDoesNotReleaseBody(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	if err := os.Mkdir(fixture.st.Path(state.ParentEvidenceLedgerLockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	scope, err := parentevidence.CaptureReadScope(fixture.st)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = parentevidence.FinishReadInScope(fixture.st, scope, state.ParentEvidenceSurfaceSource, "digest", func() (int, error) {
		return stdout.WriteString("must not be delivered")
	})
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("lock failure err=%v released=%q", err, stdout.String())
	}
}

func TestParentEvidenceTotalBudgetDoesNotClaimOmittedBody(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(fixture.repoRoot, name+".md"), []byte(strings.Repeat(name, 12000)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	requests := []parentevidence.SourceRequest{
		{Question: "first source", Path: "first.md", LineStart: 1, LineEnd: 1, BudgetBytes: 80000},
		{Question: "second source", Path: "second.md", LineStart: 1, LineEnd: 1, BudgetBytes: 80000},
	}
	first := runParentEvidence(t, fixture, parentevidence.Manifest{Version: 1, Reason: "combined output budget", Source: requests})
	if first.Output.Parts[1].Status != parentevidence.PartRefinement || first.Output.Parts[1].Source.Content != "" {
		t.Fatalf("second source was not omitted: status=%s", first.Output.Parts[1].Status)
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSource, first.Output.Parts[1].Digest); err != nil || delivered {
		t.Fatalf("omitted source delivered=%v err=%v", delivered, err)
	}
	second := runParentEvidence(t, fixture, parentevidence.Manifest{Version: 1, Reason: "fetch omitted source", Source: requests[1:]})
	if second.Output.Parts[0].Source.Content == "" || second.Output.Parts[0].Status == parentevidence.PartRefinement {
		t.Fatalf("omitted source was suppressed on retry: status=%s reason=%s", second.Output.Parts[0].Status, second.Output.Parts[0].Reason)
	}
}

func TestParentEvidenceRejectsProjectionFromPreviousLease(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	projector := &parentEvidenceProjector{cfg: fixture.cfg, st: fixture.st, ownerCallID: "old-lease-call"}
	projector.project(parentevidence.Manifest{Version: 1, Reason: "old lease", Source: []parentevidence.SourceRequest{
		{Question: "source before decision", Path: "docs/guide.md", LineStart: 1, LineEnd: 2, BudgetBytes: 4096},
	}})
	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := commitParentEvidenceProjection(projector, &stdout, "lease changed")
	if err == nil || !strings.Contains(err.Error(), "scope changed") || stdout.Len() != 0 {
		t.Fatalf("stale projection err=%v released=%q", err, stdout.String())
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSource, projector.output.Parts[0].Digest); err != nil || delivered {
		t.Fatalf("old body claimed in new lease: delivered=%v err=%v", delivered, err)
	}
}

func TestStandaloneParentReadRejectsProjectionFromPreviousLease(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	scope, err := parentevidence.CaptureReadScope(fixture.st)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = parentevidence.FinishReadInScope(fixture.st, scope, state.ParentEvidenceSurfaceStatus, "old-digest", func() (int, error) {
		return stdout.WriteString("must not be delivered")
	})
	if err == nil || !strings.Contains(err.Error(), "scope changed") || stdout.Len() != 0 {
		t.Fatalf("stale standalone projection err=%v released=%q", err, stdout.String())
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, "old-digest"); err != nil || delivered {
		t.Fatalf("old standalone body claimed in new lease: delivered=%v err=%v", delivered, err)
	}
}
