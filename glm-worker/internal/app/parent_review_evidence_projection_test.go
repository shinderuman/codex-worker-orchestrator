package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
)

func TestPrintParentReviewEvidenceBuildsFromOpenBinding(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2-3")

	var stdout bytes.Buffer
	if err := parentevidence.PrintReviewEvidence(cfg.RepoRoot, st, &stdout); err != nil {
		t.Fatal(err)
	}
	var output parentevidence.Output
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Parts) != 1 || output.Parts[0].Source == nil {
		t.Fatalf("review evidence parts = %#v", output.Parts)
	}
	source := output.Parts[0].Source
	if source.Path != "review.go" || source.LineStart != 2 || source.LineEnd != 3 || source.Content == "" {
		t.Fatalf("review source = %#v", source)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("review accept readiness = %v err=%v", ready, err)
	}
}

func TestBuildParentReviewEvidenceManifestUsesSourceForSymbolTarget(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:target")

	manifest, err := parentevidence.BuildReviewManifest(cfg.RepoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Source) != 1 || len(manifest.Diff) != 0 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Source[0].Path != "review.go" || manifest.Source[0].LineStart != 1 {
		t.Fatalf("source request = %#v", manifest.Source[0])
	}
}

func TestBuildParentReviewEvidenceManifestRequiresOpenReview(t *testing.T) {
	cfg, st, _ := newParentEvidenceReviewStore(t)
	if _, err := parentevidence.BuildReviewManifest(cfg.RepoRoot, st); err == nil {
		t.Fatal("review evidence builder accepted a missing open review")
	}
}

func TestBuildParentReviewEvidenceManifestRejectsAmbiguousTarget(t *testing.T) {
	_, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "inspect-current-review")
	t.Fatal("unreachable: ambiguous review target should be rejected before a binding is persisted")
}
