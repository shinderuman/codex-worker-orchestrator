package app

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintParentReviewEvidenceBuildsFromOpenBinding(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2-3(exact target)")

	var stdout bytes.Buffer
	if err := PrintParentReviewEvidence(cfg, st, &stdout); err != nil {
		t.Fatal(err)
	}
	var output parentEvidenceOutput
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

func TestBuildParentReviewEvidenceManifestUsesDiffForSymbolTarget(t *testing.T) {
	_, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:target")

	manifest, err := buildParentReviewEvidenceManifest(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Source) != 0 || len(manifest.Diff) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Diff[0].Paths) != 1 || manifest.Diff[0].Paths[0] != "review.go" {
		t.Fatalf("diff request = %#v", manifest.Diff[0])
	}
}

func TestBuildParentReviewEvidenceManifestRequiresOpenReview(t *testing.T) {
	_, st, _ := newParentEvidenceReviewStore(t)
	if _, err := buildParentReviewEvidenceManifest(st); err == nil {
		t.Fatal("review evidence builder accepted a missing open review")
	}
}

func TestBuildParentReviewEvidenceManifestRejectsAmbiguousTarget(t *testing.T) {
	_, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "inspect-current-review")
	if _, err := buildParentReviewEvidenceManifest(st); err == nil {
		t.Fatal("review evidence builder accepted a target without path:locator")
	}
}
