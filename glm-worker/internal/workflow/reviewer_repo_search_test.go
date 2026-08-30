package workflow

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newReviewerSearchWorkflow(t *testing.T, paths []string) (*Workflow, *state.StateStore, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{RepoRoot: root, RepoHash: "review-search-routing", StateBase: t.TempDir(), RepoSearch: true}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, nil)
	w.now = func() time.Time { return time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC) }
	w.collectChangedPaths = func(string, string) ([]string, error) { return paths, nil }
	return w, st, taskID
}

func TestReviewerDiffFirstSkipsIndependentSearchForTestOnlyDiff(t *testing.T) {
	w, st, taskID := newReviewerSearchWorkflow(t, []string{"glm-worker/internal/workflow/foo_test.go"})
	calls := 0
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		calls++
		return reposearch.Report{}, nil
	}
	block := w.reviewerDiffFirstContext("fix workflow", 1)
	if calls != 0 || !strings.Contains(block, "INDEPENDENT_SEARCH: skipped") || !strings.Contains(block, "SEARCH_OUTCOME: diff-sufficient") {
		t.Fatalf("calls=%d block=%s", calls, block)
	}
	event := readOnlyTaskEvent(t, st, taskID)
	if event.Phase != reviewerRepoSearchPhase || event.Subtype != reviewerSearchDiffSufficient || event.SearchQuery != "" || len(event.SearchPaths) != 0 {
		t.Fatalf("event=%#v", event)
	}
}

func TestReviewerDiffFirstSearchesImpactIndependentlyAndExcludesChangedPaths(t *testing.T) {
	w, st, taskID := newReviewerSearchWorkflow(t, []string{"glm-worker/internal/workflow/workflow.go"})
	w.repoSearch = func(_ context.Context, _ string, query string, opts reposearch.Options) (reposearch.Report, error) {
		if !strings.Contains(query, "review impact paths: glm-worker/internal/workflow/workflow.go") || opts.MaxResults != RepoSearchMaxResults {
			t.Fatalf("query=%q opts=%#v", query, opts)
		}
		return reposearch.Report{Results: []reposearch.Result{
			{Path: "glm-worker/internal/workflow/workflow.go", Line: 50},
			{Path: "glm-worker/internal/workflow/prompts.go", Line: 20},
		}}, nil
	}
	block := w.reviewerDiffFirstContext("change reviewer dispatch", 2)
	if !strings.Contains(block, "INDEPENDENT_SEARCH: performed") || !strings.Contains(block, "IMPACT_CANDIDATE: glm-worker/internal/workflow/prompts.go:20") {
		t.Fatalf("block=%s", block)
	}
	if strings.Contains(block, "IMPACT_CANDIDATE: glm-worker/internal/workflow/workflow.go") {
		t.Fatalf("changed path leaked into impact candidates: %s", block)
	}
	if !strings.Contains(block, "WORKER_SEARCH_AUTHORITY: none") {
		t.Fatalf("independence marker missing: %s", block)
	}
	event := readOnlyTaskEvent(t, st, taskID)
	if event.Phase != reviewerRepoSearchPhase || event.Subtype != reviewerSearchHit || event.SearchQuery != "" || len(event.SearchPaths) != 1 || event.SearchPaths[0] != "glm-worker/internal/workflow/prompts.go" {
		t.Fatalf("event=%#v", event)
	}
}

func TestReviewerDiffFirstExcludesParentManagedMetadataFromImpactTerms(t *testing.T) {
	paths := []string{"glm-worker/internal/workflow/workflow.go", state.ParentPlanFile}
	w, st, _ := newReviewerSearchWorkflow(t, paths)
	root := w.config.RepoRoot
	gitScope(t, root, "init")
	gitScope(t, root, "config", "user.email", "review-search@example.invalid")
	gitScope(t, root, "config", "user.name", "review-search-test")
	writeScopeFile(t, root, "glm-worker/internal/workflow/workflow.go", "package workflow\nvar implementationNeedle = 1\n")
	writeScopeFile(t, root, state.ParentPlanFile, "PARENT_METADATA_BASELINE\n")
	gitScope(t, root, "add", ".")
	gitScope(t, root, "commit", "-m", "baseline")
	baseline, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-head", strings.TrimSpace(string(baseline))); err != nil {
		t.Fatal(err)
	}
	writeScopeFile(t, root, "glm-worker/internal/workflow/workflow.go", "package workflow\nvar implementationNeedle = 2\n")
	writeScopeFile(t, root, state.ParentPlanFile, "PARENT_METADATA_NEEDLE\n")

	w.repoSearch = func(_ context.Context, _ string, query string, _ reposearch.Options) (reposearch.Report, error) {
		if !strings.Contains(query, "implementationNeedle") {
			t.Fatalf("implementation diff term missing: %q", query)
		}
		if strings.Contains(query, "PARENT_METADATA_NEEDLE") || strings.Contains(query, "PARENT_METADATA_BASELINE") {
			t.Fatalf("parent-managed metadata leaked into query: %q", query)
		}
		return reposearch.Report{}, nil
	}
	block := w.reviewerDiffFirstContext("review implementation", 1)
	if !strings.Contains(block, "CHANGED_PATH: "+state.ParentPlanFile) {
		t.Fatalf("changed-path evidence lost parent metadata: %s", block)
	}
}

func TestReviewerDiffFirstSearchFailureFallsBackToDiffInspection(t *testing.T) {
	w, _, _ := newReviewerSearchWorkflow(t, []string{"glm-worker/internal/workflow/workflow.go"})
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		return reposearch.Report{}, errors.New("search unavailable")
	}
	block := w.reviewerDiffFirstContext("change reviewer dispatch", 1)
	if !strings.Contains(block, "SEARCH_OUTCOME: independent-search-error-fallback") || !strings.Contains(block, "CHANGED_PATH: glm-worker/internal/workflow/workflow.go") {
		t.Fatalf("block=%s", block)
	}
}

func TestReviewerDiffFirstRecordsRepoSearchStatsWithDuration(t *testing.T) {
	w, st, taskID := newReviewerSearchWorkflow(t, []string{"glm-worker/internal/workflow/workflow.go"})
	current := time.Date(2026, 8, 30, 3, 0, 0, 0, time.UTC)
	w.now = func() time.Time {
		current = current.Add(100 * time.Millisecond)
		return current
	}
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		return reposearch.Report{Results: []reposearch.Result{
			{Path: "glm-worker/internal/workflow/workflow.go", Line: 50},
			{Path: "glm-worker/internal/workflow/prompts.go", Line: 20},
		}}, nil
	}
	w.reviewerDiffFirstContext("change reviewer dispatch", 1)

	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.RepoSearchCalls != 1 || stats.RepoSearchQueriesByCategory[reviewerRepoSearchPhase] != 1 {
		t.Fatalf("repo-search stats = %+v", stats)
	}
	if stats.RepoSearchOutcomes[reviewerSearchHit] != 1 || stats.RepoSearchResults != 1 || stats.RepoSearchDurationMS != 100 {
		t.Fatalf("repo-search outcomes = %+v", stats)
	}
	events := readAllTaskEvents(t, st, taskID)
	if len(events) != 1 || events[0].DurationMS != 100 {
		t.Fatalf("events = %+v", events)
	}
}

func TestReviewerDiffFirstDisabledKeepsDiffNavigationWithoutSearch(t *testing.T) {
	w, st, taskID := newReviewerSearchWorkflow(t, []string{"glm-worker/internal/workflow/workflow.go"})
	w.config.RepoSearch = false
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		t.Fatal("disabled flagでsearchが実行されました")
		return reposearch.Report{}, nil
	}
	block := w.reviewerDiffFirstContext("change reviewer dispatch", 1)
	if !strings.Contains(block, "CHANGED_PATH: glm-worker/internal/workflow/workflow.go") {
		t.Fatalf("diff changed-path navigationが消失しました: %s", block)
	}
	if !strings.Contains(block, "INDEPENDENT_SEARCH: skipped") || !strings.Contains(block, "SEARCH_OUTCOME: independent-search-disabled") {
		t.Fatalf("disabled outcomeが明示されていません: %s", block)
	}
	if strings.Contains(block, "INDEPENDENT_QUERY") || strings.Contains(block, "IMPACT_CANDIDATE") {
		t.Fatalf("disabled blockへsearch結果が混入しています: %s", block)
	}
	if _, err := os.Stat(st.TaskEventLogPath(taskID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled flagでsearch eventが記録されました: %v", err)
	}
}

func TestReviewerPromptMakesDiffFirstNavigationProductionVisible(t *testing.T) {
	navigation := renderReviewerDiffFirstNavigation(
		[]string{"glm-worker/internal/workflow/workflow.go"},
		reviewerSearchHit,
		"independent query",
		[]reposearch.Result{{Path: "glm-worker/internal/workflow/prompts.go", Line: 20}},
	)
	prompt := reviewerPrompt("request", "none", "worker report", 1, "baseline", navigation, "")
	for _, want := range []string{
		"REVIEW_MODE: INDEPENDENT_REVIEW",
		"REVIEW_DIFF_FIRST_NAVIGATION:",
		"CHANGED_PATH: glm-worker/internal/workflow/workflow.go",
		"IMPACT_CANDIDATE: glm-worker/internal/workflow/prompts.go:20",
		"actual git diffを最初に確認",
		"worker search結果やworker reportをauthorityとして採用せず",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in %s", want, prompt)
		}
	}
}

func readOnlyTaskEvent(t *testing.T, st *state.StateStore, taskID string) state.TaskEventRecord {
	t.Helper()
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("event missing: %v", scanner.Err())
	}
	event, err := state.ParseTaskEventLine(scanner.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if scanner.Scan() {
		t.Fatalf("unexpected extra event: %q", scanner.Text())
	}
	return event
}
