package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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
	Question     string             `json:"question,omitempty"`
	Scopes       []string           `json:"scopes,omitempty"`
	BudgetBytes  int                `json:"budget_bytes,omitempty"`
	Candidates   int                `json:"candidates,omitempty"`
	ResultCount  int                `json:"result_count"`
	Reason       string             `json:"reason,omitempty"`
	CacheStatus  string             `json:"cache_status,omitempty"`
	IndexedFiles int                `json:"indexed_files"`
	SkippedFiles int                `json:"skipped_files"`
	Results      []repoSearchResult `json:"results"`
}

type repoSearchRequest struct {
	Question    string
	Scopes      []string
	BudgetBytes int
}

const (
	repoSearchResultDisabled = "disabled"
	repoSearchResultEmpty    = "empty"
	repoSearchResultHit      = "hit"
	repoSearchResultRequired = "refinement_required"

	repoSearchCandidateLimit = 50

	repoSearchSymbolScopePrefix = "symbol:"
)

func (r repoSearchRequest) pathPrefixes() []string {
	prefixes := make([]string, 0, len(r.Scopes))
	for _, scope := range r.Scopes {
		if strings.HasPrefix(scope, repoSearchSymbolScopePrefix) {
			continue
		}
		prefixes = append(prefixes, scope)
	}
	return prefixes
}

func (r repoSearchRequest) symbols() []string {
	symbols := make([]string, 0, len(r.Scopes))
	for _, scope := range r.Scopes {
		if strings.HasPrefix(scope, repoSearchSymbolScopePrefix) {
			symbols = append(symbols, strings.TrimPrefix(scope, repoSearchSymbolScopePrefix))
		}
	}
	return symbols
}

func printRepoSearch(request repoSearchRequest, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	if !cfg.RepoSearch {
		return writeJSON(stdout, repoSearchOutput{Status: repoSearchResultDisabled, Result: repoSearchResultDisabled, Results: []repoSearchResult{}})
	}
	report, err := reposearch.Search(context.Background(), cfg.RepoRoot, request.Question, reposearch.Options{
		DisableCache: true,
		MaxResults:   workflow.RepoSearchMaxResults,
		PathPrefixes: request.pathPrefixes(),
		Symbols:      request.symbols(),
	})
	if err != nil {
		return fmt.Errorf("repo searchが失敗しました: %w", err)
	}
	results := repoSearchResults(report.Results)
	digest := parentEvidenceStringDigest(
		state.ParentEvidenceSurfaceSearch,
		request.Question,
		fmt.Sprintf("%v", request.Scopes),
		repoSearchResultsDigest(results),
	)
	return withParentEvidenceLedgerLock(st, func() error {
		return serveRepoSearchResult(st, request, report, results, digest, stdout)
	})
}

func serveRepoSearchResult(st *state.StateStore, request repoSearchRequest, report reposearch.Report, results []repoSearchResult, digest string, stdout io.Writer) error {
	decision, entry, decisionErr := decideParentRead(st, state.ParentEvidenceSurfaceSearch, digest)
	if decisionErr != nil {
		return decisionErr
	}
	if decision == parentReadDuplicate {
		recordParentEvidence(st, state.ParentEvidenceRecord{
			Surface: state.ParentEvidenceSurfaceSearch, Origin: state.ParentEvidenceOriginStandalone,
			Digest: digest, Outcome: state.ParentEvidenceOutcomeDuplicate,
			Reason: parentEvidenceUnchangedReason, OwnerCallID: entry.OwnerCallID,
		})
		return &DuplicateParentProjectionError{Surface: state.ParentEvidenceSurfaceSearch, Digest: digest, OwnerCallID: entry.OwnerCallID}
	}
	output := buildRepoSearchOutput(request, report, results)
	if output.Status == repoSearchResultRequired {
		if writeErr := writeJSON(stdout, output); writeErr != nil {
			return writeErr
		}
		if err := saveParentEvidenceLedger(st, state.ParentEvidenceSurfaceSearch, digest, state.ParentEvidenceOriginStandalone, ""); err != nil {
			return err
		}
		rendered, _ := json.Marshal(output)
		recordParentEvidence(st, state.ParentEvidenceRecord{
			Surface: state.ParentEvidenceSurfaceSearch, Origin: state.ParentEvidenceOriginStandalone,
			Digest: digest, Bytes: len(rendered), Outcome: state.ParentEvidenceOutcomeRefinement,
			Reason: output.Reason,
		})
		return nil
	}
	written, writeErr := writeMeasuredJSON(stdout, output)
	if writeErr != nil {
		return writeErr
	}
	if err := saveParentEvidenceLedger(st, state.ParentEvidenceSurfaceSearch, digest, state.ParentEvidenceOriginStandalone, ""); err != nil {
		return err
	}
	recordParentEvidence(st, state.ParentEvidenceRecord{
		Surface: state.ParentEvidenceSurfaceSearch, Origin: state.ParentEvidenceOriginStandalone,
		Digest: digest, Bytes: written, Outcome: state.ParentEvidenceOutcomeProjected,
	})
	return nil
}

func buildRepoSearchOutput(request repoSearchRequest, report reposearch.Report, results []repoSearchResult) repoSearchOutput {
	output := repoSearchOutput{
		Question:     request.Question,
		Scopes:       request.Scopes,
		BudgetBytes:  request.BudgetBytes,
		Candidates:   report.Candidates,
		ResultCount:  len(results),
		CacheStatus:  string(report.CacheStatus),
		IndexedFiles: report.IndexedFiles,
		SkippedFiles: report.SkippedFiles,
		Results:      []repoSearchResult{},
	}
	if report.Candidates > repoSearchCandidateLimit {
		output.Status = repoSearchResultRequired
		output.Result = repoSearchResultRequired
		output.Reason = fmt.Sprintf(
			"candidates %d exceed the limit %d; add --scope <path> or --scope symbol:<identifier> to narrow the search before reading bodies",
			report.Candidates, repoSearchCandidateLimit,
		)
		return output
	}
	rendered, err := json.Marshal(results)
	if err == nil && len(rendered) > request.BudgetBytes {
		output.Status = repoSearchResultRequired
		output.Result = repoSearchResultRequired
		output.Reason = fmt.Sprintf(
			"result locators need %d bytes but the budget is %d; narrow --scope or raise --budget instead of receiving a truncated body",
			len(rendered), request.BudgetBytes,
		)
		return output
	}
	output.Status = "executed"
	output.Result = repoSearchResultEmpty
	if len(results) > 0 {
		output.Result = repoSearchResultHit
		output.Results = results
	}
	return output
}

func repoSearchResultsDigest(results []repoSearchResult) string {
	parts := make([]string, 0, len(results)*2)
	for _, result := range results {
		parts = append(parts, result.Path, strconv.Itoa(result.Line))
	}
	return parentEvidenceStringDigest(parts...)
}

func repoSearchResults(results []reposearch.Result) []repoSearchResult {
	converted := make([]repoSearchResult, 0, len(results))
	for _, result := range results {
		converted = append(converted, repoSearchResult{Path: result.Path, Line: result.Line, Score: result.Score})
	}
	return converted
}
