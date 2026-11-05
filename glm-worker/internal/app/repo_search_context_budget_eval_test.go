package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

func TestRepoSearchContextBudgetParentLocatorProjection(t *testing.T) {
	source := []reposearch.Result{
		{Path: "glm-worker/internal/reposearch/cache.go", Line: 42, Score: 3.5, Snippet: "duplicate source line"},
		{Path: "glm-worker/internal/reposearch/score.go", Line: 63, Score: 2.0, Snippet: "duplicate source line"},
	}
	request := repoSearchRequest{
		Question:    "cache",
		Scopes:      []string{"glm-worker/internal/reposearch"},
		BudgetBytes: 4096,
	}
	report := reposearch.Report{Candidates: 2}
	locators := repoSearchResults(source)
	original := buildRepoSearchOutput(request, report, locators)
	if original.Status != "executed" || original.ResultCount != 2 || len(original.Results) != 2 {
		t.Fatalf("unexpected parent projection: %#v", original)
	}
	visible, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(visible, []byte("duplicate source line")) || bytes.Contains(visible, []byte("\"snippet\"")) {
		t.Fatalf("source snippets leaked into parent model-visible JSON: %s", visible)
	}

	source[0].Snippet = "a different source snippet"
	source[1].Snippet = ""
	modified, err := json.Marshal(buildRepoSearchOutput(request, report, repoSearchResults(source)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(visible, modified) {
		t.Fatalf("parent output depends on hidden source snippets: before=%s after=%s", visible, modified)
	}

	serializedLocators, err := json.Marshal(locators)
	if err != nil {
		t.Fatal(err)
	}
	request.BudgetBytes = len(serializedLocators) - 1
	refinement := buildRepoSearchOutput(request, report, locators)
	if refinement.Status != repoSearchResultRequired || refinement.ResultCount != 2 || len(refinement.Results) != 0 {
		t.Fatalf("budget overflow did not require refinement: %#v", refinement)
	}

	t.Logf("parent model-visible JSON bytes=%d; source snippet bytes in parent projection=0; exact duplicate source snippets=2, duplicate locator paths=0", len(visible))
	t.Log("parent search still requires scope refinement for a too-small locator budget; actual follow-up reads and Codex/Sol Quality Delta are unmeasured")
}
