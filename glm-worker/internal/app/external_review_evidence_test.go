package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentUsageRejectsEveryChainMemberWithoutUsableCounterAnchor(t *testing.T) {
	for _, position := range []string{"first", "last"} {
		for _, shape := range []string{"no-token-event", "missing-total"} {
			t.Run(position+"/"+shape, func(t *testing.T) {
				task := newAnalysisTerminalTask(t)
				firstRel := analysisRolloutRel()
				lastRel := "archived_sessions/rollout-last-" + codexTestParentThreadID + ".jsonl"
				firstAt := task.start.Add(-time.Minute)
				lastAt := task.start.Add(10 * time.Minute)
				lines := func(at time.Time, missing bool) []string {
					if !missing {
						return []string{chainTokenCountLine(t, at, 100, 50, 10, 5, 110, 110)}
					}
					if shape == "missing-total" {
						return []string{`{"timestamp":"` + at.Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":110}}}}`}
					}
					return []string{parentUsageToolCallLine(t, at, "without-counter")}
				}
				writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, firstAt.Add(-time.Minute), "/repo", "Codex Desktop", lines(firstAt, position == "first"))
				writeChainRollout(t, task.codexHome, lastRel, codexTestParentThreadID, lastAt.Add(-time.Minute), "/repo", "Codex Desktop", lines(lastAt, position == "last"))
				report := runParentUsageReport(t, task.cfg)
				if report.ParentSession.Status != codexStatusAmbiguous || !strings.Contains(report.ParentSession.Detail, "no usable token counter anchor") {
					t.Fatalf("parent session = %#v", report.ParentSession)
				}
				if report.Intervals.TaskExecution.Tokens.Status != codexStatusAmbiguous {
					t.Fatalf("missing counter produced token totals: %#v", report.Intervals.TaskExecution.Tokens)
				}
			})
		}
	}
}

func TestAnalysisMissingCollectedChainMemberIsUnreadable(t *testing.T) {
	start := time.Now().UTC()
	association := codexAssociation{
		ParentStatus: codexStatusIncluded, ParentThreadID: codexTestParentThreadID,
		ParentChain: []codexRollout{{HomeRelative: "first.jsonl"}, {HomeRelative: "second.jsonl"}},
	}
	collector := newBundleCollector()
	collector.entries[codexRolloutArchivePathAt(codexTestParentThreadID, 0)] = bundleEntry{}
	scan, err := scanAnalysisRolloutWindow(collector, association, start, start.Add(time.Hour))
	missingPath := codexRolloutArchivePathAt(codexTestParentThreadID, 1)
	if err == nil || !strings.Contains(err.Error(), missingPath) {
		t.Fatalf("missing collected entry error = %v, want %s", err, missingPath)
	}
	if window := analysisRolloutWindow(association, scan, err, start); window.Status != analysisStatusUnreadable {
		t.Fatalf("missing evidence window = %#v", window)
	}
}

func TestParentEvidenceLedgerLockFailureDoesNotReleaseBody(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	if err := os.Mkdir(fixture.st.Path(state.ParentEvidenceLedgerLockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := finishParentRead(fixture.st, state.ParentEvidenceSurfaceSource, "digest", func() (int, error) {
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
	requests := []parentEvidenceSourceRequest{
		{Question: "first source", Path: "first.md", LineStart: 1, LineEnd: 1, BudgetBytes: 80000},
		{Question: "second source", Path: "second.md", LineStart: 1, LineEnd: 1, BudgetBytes: 80000},
	}
	first := runParentEvidence(t, fixture, parentEvidenceManifest{Version: 1, Reason: "combined output budget", Source: requests})
	if first.Output.Parts[1].Status != parentEvidencePartRefinement || first.Output.Parts[1].Source.Content != "" {
		t.Fatalf("second source was not omitted: status=%s", first.Output.Parts[1].Status)
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSource, first.Output.Parts[1].Digest); err != nil || delivered {
		t.Fatalf("omitted source delivered=%v err=%v", delivered, err)
	}
	second := runParentEvidence(t, fixture, parentEvidenceManifest{Version: 1, Reason: "fetch omitted source", Source: requests[1:]})
	if second.Output.Parts[0].Source.Content == "" || second.Output.Parts[0].Status == parentEvidencePartRefinement {
		t.Fatalf("omitted source was suppressed on retry: status=%s reason=%s", second.Output.Parts[0].Status, second.Output.Parts[0].Reason)
	}
}

func TestParentEvidenceRejectsProjectionFromPreviousLease(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	projector := &parentEvidenceProjector{cfg: fixture.cfg, st: fixture.st, ownerCallID: "old-lease-call"}
	projector.project(parentEvidenceManifest{Version: 1, Reason: "old lease", Source: []parentEvidenceSourceRequest{
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
	scope, err := captureParentEvidenceReadScope(fixture.st)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = finishParentReadInScope(fixture.st, scope, state.ParentEvidenceSurfaceStatus, "old-digest", func() (int, error) {
		return stdout.WriteString("must not be delivered")
	})
	if err == nil || !strings.Contains(err.Error(), "scope changed") || stdout.Len() != 0 {
		t.Fatalf("stale standalone projection err=%v released=%q", err, stdout.String())
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, "old-digest"); err != nil || delivered {
		t.Fatalf("old standalone body claimed in new lease: delivered=%v err=%v", delivered, err)
	}
}
