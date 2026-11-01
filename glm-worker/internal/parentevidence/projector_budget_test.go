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
			WorkingDir: strings.Repeat("長いパス/", 45),
			Form: "go-test", Status: "pass",
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
