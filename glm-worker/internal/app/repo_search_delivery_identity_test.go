package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRepoSearchRefinementDeliversLocatorsWithinSameLease(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	request := repoSearchRequest{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 8}
	lease, err := fixture.st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	serve := func(budget int) (repoSearchOutput, int, error) {
		request.BudgetBytes = budget
		var stdout bytes.Buffer
		err := printRepoSearchWithSearch(request, fixture.cfg, fixture.st, &stdout,
			func(ctx context.Context, root, question string, options reposearch.Options) (reposearch.Report, error) {
				calls++
				return reposearch.Search(ctx, root, question, options)
			})
		var output repoSearchOutput
		if err == nil {
			if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
				t.Fatal(decodeErr)
			}
		}
		return output, stdout.Len(), err
	}

	first, firstBytes, err := serve(8)
	if err != nil || first.Status != repoSearchResultRequired || len(first.Results) != 0 || first.ResultCount == 0 {
		t.Fatalf("first refinement = %#v, bytes=%d, err=%v", first, firstBytes, err)
	}
	_, repeatBytes, err := serve(8)
	var duplicate *parentevidence.DuplicateProjectionError
	if !errors.As(err, &duplicate) || repeatBytes != 0 {
		t.Fatalf("repeated short budget: bytes=%d err=%v", repeatBytes, err)
	}
	delivered, locatorBytes, err := serve(4096)
	if err != nil || delivered.Result != repoSearchResultHit || len(delivered.Results) == 0 {
		t.Fatalf("raised budget: output=%#v bytes=%d err=%v", delivered, locatorBytes, err)
	}
	_, againBytes, err := serve(4096)
	if !errors.As(err, &duplicate) || againBytes != 0 {
		t.Fatalf("repeated sufficient budget: bytes=%d err=%v", againBytes, err)
	}
	after, err := fixture.st.ParentEvidenceLeaseEpoch()
	if err != nil || after != lease {
		t.Fatalf("lease changed from %d to %d: %v", lease, after, err)
	}
	records, err := fixture.st.ReadParentEvidence()
	if err != nil {
		t.Fatal(err)
	}
	var refinements, projections, duplicates int
	for _, record := range records {
		if record.Surface != state.ParentEvidenceSurfaceSearch {
			continue
		}
		switch record.Outcome {
		case state.ParentEvidenceOutcomeRefinement:
			refinements++
		case state.ParentEvidenceOutcomeProjected:
			projections++
		case state.ParentEvidenceOutcomeDuplicate:
			duplicates++
		}
	}
	if calls != 4 || refinements != 1 || projections != 1 || duplicates != 2 {
		t.Fatalf("calls=%d refinement=%d projected=%d duplicate=%d", calls, refinements, projections, duplicates)
	}
	t.Logf("same-lease search calls=%d; delivered refinement bytes=%d, locator bytes=%d; duplicate stdout bytes=%d; lease rotations=0", calls, firstBytes, locatorBytes, repeatBytes+againBytes)
}

func TestRepoSearchRefinementAndBatchDeliveryShareIdentity(t *testing.T) {
	tests := []struct {
		name            string
		first           string
		firstBudget     int
		second          string
		secondBudget    int
		wantDuplicate   bool
		wantRefinement  bool
	}{
		{"standalone shortage then batch shortage", "standalone", 8, "batch", 8, true, true},
		{"batch shortage then standalone shortage", "batch", 8, "standalone", 8, true, true},
		{"standalone shortage then batch locators", "standalone", 8, "batch", 4096, false, false},
		{"batch shortage then standalone locators", "batch", 8, "standalone", 4096, false, false},
		{"standalone locators then batch locators", "standalone", 4096, "batch", 4096, true, false},
		{"batch locators then standalone locators", "batch", 4096, "standalone", 4096, true, false},
		{"batch shortage then batch shortage", "batch", 8, "batch", 8, true, true},
		{"batch shortage then batch locators", "batch", 8, "batch", 4096, false, false},
		{"batch locators then batch locators", "batch", 4096, "batch", 4096, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newParentEvidenceFixture(t)
			serve := func(kind string, budget int) (string, int, error) {
				request := repoSearchRequest{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: budget}
				if kind == "standalone" {
					var out bytes.Buffer
					err := printRepoSearch(request, fixture.cfg, fixture.st, &out)
					if err != nil {
						return "", out.Len(), err
					}
					var result repoSearchOutput
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Status == repoSearchResultRequired && len(result.Results) != 0 {
						t.Fatalf("short budget delivered locators: %#v", result)
					}
					if result.Status == "executed" && len(result.Results) == 0 {
						t.Fatalf("sufficient budget did not deliver locators: %#v", result)
					}
					return result.Status, out.Len(), nil
				}
				batch := runParentEvidence(t, fixture, parentevidence.Manifest{
					Version: 1, Reason: "search delivery identity",
					Search: []parentevidence.SearchRequest{{
						Question: request.Question, Scopes: request.Scopes, BudgetBytes: budget,
					}},
				})
				if len(batch.Output.Parts) != 1 {
					t.Fatalf("batch parts: %s", batch.Raw)
				}
				part := batch.Output.Parts[0]
				if part.Reason == parentevidence.UnchangedReason {
					if part.Search == nil || len(part.Search.Results) != 0 {
						t.Fatalf("duplicate batch retained results: %#v", part)
					}
					return "duplicate", len(batch.Raw), nil
				}
				if part.Status == parentevidence.PartRefinement {
					if part.Search == nil || len(part.Search.Results) != 0 {
						t.Fatalf("batch refinement retained results: %#v", part)
					}
					return repoSearchResultRequired, len(batch.Raw), nil
				}
				if part.Status != parentevidence.PartProjected || part.Search == nil || len(part.Search.Results) == 0 {
					t.Fatalf("batch did not deliver locators: %#v", part)
				}
				return "executed", len(batch.Raw), nil
			}
			firstStatus, firstBytes, firstErr := serve(tt.first, tt.firstBudget)
			if firstErr != nil || firstBytes == 0 {
				t.Fatalf("first delivery failed: status=%s bytes=%d err=%v", firstStatus, firstBytes, firstErr)
			}
			secondStatus, secondBytes, secondErr := serve(tt.second, tt.secondBudget)
			if tt.wantDuplicate {
				if tt.second == "standalone" {
					var duplicate *parentevidence.DuplicateProjectionError
					if !errors.As(secondErr, &duplicate) || secondBytes != 0 {
						t.Fatalf("duplicate standalone status=%s bytes=%d err=%v", secondStatus, secondBytes, secondErr)
					}
				} else if secondErr != nil || secondStatus != "duplicate" {
					t.Fatalf("duplicate batch status=%s bytes=%d err=%v", secondStatus, secondBytes, secondErr)
				}
			} else {
				wantStatus := "executed"
				if tt.wantRefinement {
					wantStatus = repoSearchResultRequired
				}
				if secondErr != nil || secondStatus != wantStatus || secondBytes == 0 {
					t.Fatalf("delivery status=%s bytes=%d err=%v want=%s", secondStatus, secondBytes, secondErr, wantStatus)
				}
			}
			t.Logf("first=%s/%dB, second=%s/%dB", firstStatus, firstBytes, secondStatus, secondBytes)
		})
	}
}

func TestRepoSearchDeliveryIdentityExcludesBudgetButIncludesDeliveryState(t *testing.T) {
	results := []repoSearchResult{{Path: "docs/guide.md", Line: 1, Score: 1.2}}
	request := repoSearchRequest{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 1}
	report := reposearch.Report{Candidates: 1}
	refined := buildRepoSearchOutput(request, report, results)
	if refined.Status != repoSearchResultRequired {
		t.Fatal("small-budget request did not require refinement")
	}
	refinementDigest := repoSearchDeliveryDigest(request, results, refined.Status)
	request.BudgetBytes = 8
	repeated := buildRepoSearchOutput(request, report, results)
	if repeated.Status != repoSearchResultRequired || repoSearchDeliveryDigest(request, results, repeated.Status) != refinementDigest {
		t.Fatal("changing an insufficient budget changed refinement delivery identity")
	}
	request.BudgetBytes = 4096
	delivered := buildRepoSearchOutput(request, report, results)
	if delivered.Status != "executed" || repoSearchDeliveryDigest(request, results, delivered.Status) == refinementDigest {
		t.Fatal("delivered locators reused undelivered refinement identity")
	}
}
