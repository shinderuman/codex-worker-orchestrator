package app

import (
	"context"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type repoSearchResult struct {
	Path  string  `json:"path"`
	Line  int     `json:"line"`
	Score float64 `json:"score"`
}

type repoSearchOutput struct {
	Status       string             `json:"status"`
	Result       string             `json:"result"`
	Query        string             `json:"query,omitempty"`
	ResultCount  int                `json:"result_count"`
	CacheStatus  string             `json:"cache_status,omitempty"`
	IndexedFiles int                `json:"indexed_files"`
	SkippedFiles int                `json:"skipped_files"`
	Results      []repoSearchResult `json:"results"`
}

func printRepoSearch(query string, cfg config.AppConfig, stdout io.Writer) error {
	if !cfg.RepoSearch {
		return writeJSON(stdout, repoSearchOutput{Status: "disabled", Result: "disabled", Results: []repoSearchResult{}})
	}
	report, err := reposearch.Search(context.Background(), cfg.RepoRoot, query, reposearch.Options{MaxResults: workflow.RepoSearchMaxResults})
	if err != nil {
		return fmt.Errorf("repo searchが失敗しました: %w", err)
	}
	result := "empty"
	if len(report.Results) > 0 {
		result = "hit"
	}
	return writeJSON(stdout, repoSearchOutput{
		Status:       "executed",
		Result:       result,
		Query:        query,
		ResultCount:  len(report.Results),
		CacheStatus:  string(report.CacheStatus),
		IndexedFiles: report.IndexedFiles,
		SkippedFiles: report.SkippedFiles,
		Results:      repoSearchResults(report.Results),
	})
}

func repoSearchResults(results []reposearch.Result) []repoSearchResult {
	converted := make([]repoSearchResult, 0, len(results))
	for _, result := range results {
		converted = append(converted, repoSearchResult{Path: result.Path, Line: result.Line, Score: result.Score})
	}
	return converted
}
