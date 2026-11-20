package parentevidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestApplyTotalBudgetRejectsOversizedMetadataWithoutPartClaims(t *testing.T) {
	t.Parallel()
	output := Output{Version: ManifestVersion, Status: StatusOK, Parts: make([]Part, 480)}
	for i := range output.Parts {
		output.Parts[i] = Part{
			Kind: "source", Surface: "source", Detail: strings.Repeat("metadata", 35),
			Status: PartProjected, Digest: fmt.Sprintf("%064d", i+1),
			Locator: fmt.Sprintf("path/%03d", i),
		}
	}
	if outputSize(output) <= MaxOutputBytes {
		t.Fatal("480-part metadata fixture must exceed the output budget")
	}
	claim := state.ParentEvidenceLedgerEntry{Surface: "source", Digest: output.Parts[0].Digest}
	if applyTotalBudget(&output) {
		t.Fatal("oversized metadata was accepted for delivery")
	}
	requireBudgetedJSON(t, output)
	if output.Status != StatusError || len(output.Parts) != 1 || output.Parts[0].Status != PartError {
		t.Fatalf("oversized output not converted into bounded error: %#v", output)
	}
	if claimSurvivesBudget(output.Parts, claim) {
		t.Fatal("omitted part was marked as deliverable")
	}
}

func TestApplyTotalBudgetRecomputesStatusAfterBodyStripped(t *testing.T) {
	t.Parallel()
	output := Output{
		Version: ManifestVersion, Status: StatusOK,
		Parts: []Part{{
			Kind: "source", Surface: "source", Status: PartProjected, Digest: "digest", Locator: "file.go:1-1",
			Source: &SourceBody{Question: "source", Path: "file.go", LineStart: 1, LineEnd: 1,
				Content: strings.Repeat("日本語", MaxOutputBytes/2)},
		}},
	}
	if !applyTotalBudget(&output) {
		t.Fatal("strippable source body did not fit")
	}
	requireBudgetedJSON(t, output)
	if output.Status != StatusRequired || output.Parts[0].Status != PartRefinement ||
		output.Parts[0].Source.Content != "" {
		t.Fatalf("body stripping did not preserve refinement: %#v", output)
	}
	if claimSurvivesBudget(output.Parts, state.ParentEvidenceLedgerEntry{Surface: "source", Digest: "digest"}) {
		t.Fatal("stripped source was considered delivered")
	}
}

func TestApplyTotalBudgetRejectsNonStrippableValidationMetadata(t *testing.T) {
	t.Parallel()
	output := Output{Version: ManifestVersion, Status: StatusOK,
		OwnerCallID: strings.Repeat("x", 128),
		Parts: []Part{{Kind: "validations", Status: PartProjected,
			Validations: make([]Validation, 500)}},
	}
	for i := range output.Parts[0].Validations {
		output.Parts[0].Validations[i] = Validation{
			ValidationRunID: fmt.Sprintf("%064d", i),
			WorkingDir:      strings.Repeat("長いパス/", 45),
			Form:            "go-test", Status: "pass",
		}
	}
	if outputSize(output) <= MaxOutputBytes {
		t.Fatal("validation metadata must exceed output budget")
	}
	if applyTotalBudget(&output) {
		t.Fatal("non-strippable validation metadata was accepted")
	}
	requireBudgetedJSON(t, output)
}

func TestApplyTotalBudgetRejectsOversizedLocatorAndReason(t *testing.T) {
	t.Parallel()
	output := Output{Version: ManifestVersion, Status: StatusOK,
		Reason: strings.Repeat("理由", MaxOutputBytes),
		Parts: []Part{{Kind: "source", Status: PartProjected,
			Locator: strings.Repeat("日本語", MaxOutputBytes)}},
	}
	if applyTotalBudget(&output) {
		t.Fatal("oversized reason/locator was accepted")
	}
	requireBudgetedJSON(t, output)
}

func requireBudgetedJSON(t *testing.T, output Output) {
	t.Helper()
	var buf bytes.Buffer
	if _, err := WriteMeasuredJSON(&buf, output); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > MaxOutputBytes || !json.Valid(buf.Bytes()) {
		t.Fatalf("serialized output invalid or oversized: %d bytes", buf.Len())
	}
}

func TestTotalBudgetMeasuresDeliveredJSONWithNewline(t *testing.T) {
	t.Parallel()
	part := Part{Kind: "source", Status: PartProjected, Source: &SourceBody{Content: "x"}}
	output := Output{Version: ManifestVersion, Status: StatusOK, Parts: []Part{part}}
	var empty bytes.Buffer
	base, err := WriteMeasuredJSON(&empty, output)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("<", MaxOutputBytes-base+1)
	part.Source.Content = content
	if !applyTotalBudget(&output) || output.Parts[0].Source.Content != content {
		t.Fatal("budget dropped bytes that fit the wire encoding")
	}
	var wire bytes.Buffer
	size, err := WriteMeasuredJSON(&wire, output)
	if err != nil || size != MaxOutputBytes || wire.Bytes()[wire.Len()-1] != '\n' {
		t.Fatalf("wire size=%d err=%v", size, err)
	}
}

func TestUndeliverableSourceDoesNotReserveBatchIdentity(t *testing.T) {
	repo, st := newReviewCoverageStore(t)
	projector := NewProjector(repo, st, "source-budget-order", Providers{})
	request := SourceRequest{Question: "inspect", Path: "review.go", LineStart: 1, LineEnd: 1, BudgetBytes: 1}
	roomy := request
	roomy.BudgetBytes = 1024
	projector.Project(Manifest{Version: ManifestVersion, Reason: "source budget order", Source: []SourceRequest{request, roomy}})
	if err := projector.Commit(&bytes.Buffer{}, "source budget order"); err != nil {
		t.Fatal(err)
	}
	parts := projector.Output().Parts
	if len(parts) != 2 || parts[0].Status != PartRefinement || parts[1].Source == nil || parts[1].Source.Content == "" {
		t.Fatalf("first part suppressed deliverable source: %#v", parts)
	}
	if _, delivered, err := st.ParentEvidenceDelivered(parts[1].Surface, parts[1].Digest); err != nil || !delivered {
		t.Fatalf("delivered source missing from ledger: %t, %v", delivered, err)
	}
}

func TestDuplicateSearchCannotClaimLocatorsStrippedByBatchBudget(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	const searchDigest = "search-locators"
	providers := Providers{Search: func(request SearchRequest) Part {
		digest := searchDigest
		if request.Question != "target" {
			digest = "other-locators"
		}
		results := make([]SearchResult, 20)
		for i := range results {
			results[i] = SearchResult{Path: fmt.Sprintf("%s/%d", strings.Repeat("docs/", 510), i), Line: 1}
		}
		return Part{
			Kind: "search", Status: PartProjected, Digest: digest,
			Search: &SearchBody{Question: request.Question, Scopes: request.Scopes, Results: results},
		}
	}}
	request := func(question string) SearchRequest {
		return SearchRequest{Question: question, Scopes: []string{"docs"}, BudgetBytes: SearchMaxBudgetBytes}
	}
	first := NewProjector(repoRoot, st, "batched-search", providers)
	first.Project(Manifest{Version: ManifestVersion, Reason: "batch budget",
		Search: []SearchRequest{request("other"), request("target"), request("target")}})
	if err := first.Commit(&bytes.Buffer{}, "batch budget"); err != nil {
		t.Fatal(err)
	}
	parts := first.Output().Parts
	if len(parts) != 3 || parts[1].Status != PartRefinement || len(parts[1].Search.Results) != 0 ||
		parts[2].Reason != UnchangedReason || len(parts[2].Search.Results) != 0 {
		t.Fatal("expected the target results to be omitted and the duplicate to stay suppressed")
	}
	if _, delivered, err := st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSearch, searchDigest); err != nil || delivered {
		t.Fatalf("omitted locators marked as delivered: %t, %v", delivered, err)
	}
	second := NewProjector(repoRoot, st, "refined-search", providers)
	second.Project(Manifest{Version: ManifestVersion, Reason: "retry locator delivery",
		Search: []SearchRequest{request("target")}})
	if err := second.Commit(&bytes.Buffer{}, "retry locator delivery"); err != nil {
		t.Fatal(err)
	}
	if got := second.Output().Parts[0]; got.Status != PartProjected || len(got.Search.Results) != 20 {
		t.Fatalf("retry did not deliver the target results: %s", got.Status)
	}
}
