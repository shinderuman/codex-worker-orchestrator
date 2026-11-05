package workflow

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

// This bounded evaluation uses the current repository corpus rather than
// estimating model-visible savings from the search engine's snippet length.
// The production worker/reviewer renderers are the measured output surfaces.
func TestRepoSearchContextBudgetCurrentCorpus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	rootOutput, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	root := strings.TrimSpace(string(rootOutput))
	report, err := reposearch.Search(context.Background(), root, "cache", reposearch.Options{
		DisableCache: true,
		MaxResults:   RepoSearchMaxResults,
		PathPrefixes: []string{"glm-worker/internal/reposearch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) == 0 {
		t.Fatal("representative current-corpus search returned no results")
	}

	seenPaths := make(map[string]bool, len(report.Results))
	seenSnippets := make(map[string]bool, len(report.Results))
	snippetBytes, repeatedSnippetBytes := 0, 0
	for _, result := range report.Results {
		if seenPaths[result.Path] {
			t.Fatalf("duplicate model-visible locator: %q", result.Path)
		}
		seenPaths[result.Path] = true
		if result.Snippet == "" {
			continue
		}
		snippetBytes += len(result.Snippet)
		normalized := strings.Join(strings.Fields(strings.ToLower(result.Snippet)), " ")
		if seenSnippets[normalized] {
			repeatedSnippetBytes += len(result.Snippet)
		}
		seenSnippets[normalized] = true
	}
	if snippetBytes == 0 {
		t.Fatal("search returned no source snippets to evaluate")
	}

	worker := renderRepoSearchNavigation(report.Results)
	changed := []string{"glm-worker/internal/reposearch/cache.go"}
	reviewerCandidates := excludeChangedPaths(report.Results, changed)
	reviewer := renderReviewerDiffFirstNavigation(changed, reviewerSearchHit, "cache", reviewerCandidates)

	// Changing every source snippet must leave both model-visible navigation
	// surfaces byte-identical: the current consumers never render snippets.
	changedSnippets := append([]reposearch.Result(nil), report.Results...)
	for i := range changedSnippets {
		changedSnippets[i].Snippet = "UNIQUE_SOURCE_SNIPPET_SENTINEL_" + changedSnippets[i].Path
	}
	if got := renderRepoSearchNavigation(changedSnippets); got != worker {
		t.Fatal("worker navigation started exposing search source snippets")
	}
	if got := renderReviewerDiffFirstNavigation(changed, reviewerSearchHit, "cache",
		excludeChangedPaths(changedSnippets, changed)); got != reviewer {
		t.Fatal("reviewer navigation started exposing search source snippets")
	}
	for _, result := range report.Results {
		locator := "CANDIDATE: " + result.Path
		if !strings.Contains(worker, locator) {
			t.Fatalf("worker lost a source locator: %s", result.Path)
		}
	}
	for _, result := range reviewerCandidates {
		if !strings.Contains(reviewer, "IMPACT_CANDIDATE: "+result.Path) {
			t.Fatalf("reviewer lost an independent source locator: %s", result.Path)
		}
	}
	if strings.Contains(reviewer, "IMPACT_CANDIDATE: "+changed[0]+":") {
		t.Fatal("reviewer included an edited path as an independent candidate")
	}

	t.Logf("current corpus scope=glm-worker/internal/reposearch query=cache candidates=%d returned=%d",
		report.Candidates, len(report.Results))
	t.Logf("source snippet bytes=%d exact normalized duplicate snippet bytes=%d; model-visible snippet bytes=0",
		snippetBytes, repeatedSnippetBytes)
	t.Logf("model-visible worker navigation bytes=%d reviewer diff-first navigation bytes=%d; snippet-only allocation savings=0",
		len(worker), len(reviewer))
	t.Log("follow-up search/read counts, real Codex/Sol token usage and Quality Delta are not observable in this isolated test")
}
