package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

func TestRepoSearchContextBudgetCurrentCorpus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	root, missing, err := repoSearchCorpusRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		t.Skip("current-corpus evaluation requires a git repository; no repository found")
	}
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

func repoSearchCorpusRoot(dir string) (string, bool, error) {
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return "", false, fmt.Errorf("resolve corpus directory: %w", err)
	}
	physicalDir, err := filepath.EvalSymlinks(absoluteDir)
	if err != nil {
		return "", false, fmt.Errorf("resolve physical corpus directory: %w", err)
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = physicalDir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) &&
			strings.Contains(string(exitErr.Stderr), "not a git repository") &&
			os.Getenv("GIT_DIR") == "" && os.Getenv("GIT_WORK_TREE") == "" &&
			!hasGitMarkerInAncestors(physicalDir) {
			return "", true, nil
		}
		stderr := ""
		if exitErr != nil {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", false, fmt.Errorf("resolve repository root: %w (stderr: %s)", err, stderr)
	}
	return strings.TrimSpace(string(output)), false, nil
}

func hasGitMarkerInAncestors(dir string) bool {
	for {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func TestRepoSearchContextBudgetRepositoryDetection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	archive := t.TempDir()
	root, missing, err := repoSearchCorpusRoot(archive)
	if err != nil || !missing || root != "" {
		t.Fatalf("source archive: root=%q missing=%t err=%v; want missing repository", root, missing, err)
	}

	init := exec.Command("git", "init", "-q", archive)
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v: %s", err, output)
	}
	root, missing, err = repoSearchCorpusRoot(archive)
	if err != nil || missing {
		t.Fatalf("real repository: root=%q missing=%t err=%v", root, missing, err)
	}
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	canonicalArchive, archiveErr := filepath.EvalSymlinks(archive)
	if rootErr != nil || archiveErr != nil || canonicalRoot != canonicalArchive {
		t.Fatalf("real repository root=%q (err=%v), want %q (err=%v)", canonicalRoot, rootErr, canonicalArchive, archiveErr)
	}

	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, ".git"), []byte("invalid gitfile"), 0600); err != nil {
		t.Fatal(err)
	}
	_, missing, err = repoSearchCorpusRoot(corrupt)
	if err == nil || missing {
		t.Fatalf("corrupt repository marker: missing=%t err=%v; want hard failure", missing, err)
	}

	brokenRepo := filepath.Join(t.TempDir(), "broken-repository")
	if output, err := exec.Command("git", "init", "-q", brokenRepo).CombinedOutput(); err != nil {
		t.Fatalf("initialize broken repository fixture: %v: %s", err, output)
	}
	if err := os.Mkdir(filepath.Join(brokenRepo, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(brokenRepo, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "archive")
	if err := os.Symlink(filepath.Join(brokenRepo, "nested"), alias); err != nil {
		t.Fatalf("create repository alias: %v", err)
	}
	_, missing, err = repoSearchCorpusRoot(alias)
	if err == nil || missing {
		t.Fatalf("broken repository via symlink: missing=%t err=%v; want hard failure", missing, err)
	}

	brokenAlias := filepath.Join(t.TempDir(), "missing")
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), brokenAlias); err != nil {
		t.Fatalf("create dangling directory alias: %v", err)
	}
	_, missing, err = repoSearchCorpusRoot(brokenAlias)
	if err == nil || missing {
		t.Fatalf("unresolvable directory alias: missing=%t err=%v; want hard failure", missing, err)
	}

	t.Setenv("GIT_DIR", filepath.Join(archive, "missing.git"))
	_, missing, err = repoSearchCorpusRoot(archive)
	if err == nil || missing {
		t.Fatalf("invalid explicit GIT_DIR: missing=%t err=%v; want hard failure", missing, err)
	}
}
