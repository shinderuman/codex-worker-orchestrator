package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type repoSearchFunc func(context.Context, string, string, reposearch.Options) (reposearch.Report, error)

type repoSearchTimer struct {
	search  repoSearchFunc
	now     func() time.Time
	elapsed time.Duration
}

const (
	repoSearchPhase         = state.RepoSearchCategoryWorkerNavigation
	repoSearchKnownSkip     = state.RepoSearchOutcomeKnownTargetSkip
	repoSearchHit           = state.RepoSearchOutcomeSearchHit
	repoSearchEmptyFallback = state.RepoSearchOutcomeSearchEmptyFallback
	repoSearchErrorFallback = state.RepoSearchOutcomeSearchErrorFallback
	RepoSearchMaxResults    = 8
)

func (w *Workflow) newRepoSearchTimer() *repoSearchTimer {
	search := w.repoSearch
	if search == nil {
		search = reposearch.Search
	}
	return &repoSearchTimer{search: search, now: w.now}
}

func (t *repoSearchTimer) run(ctx context.Context, repoRoot string, query string, opts reposearch.Options) (reposearch.Report, error) {
	started := t.now()
	report, err := t.search(ctx, repoRoot, query, opts)
	t.elapsed = t.now().Sub(started)
	return report, err
}

func (w *Workflow) newWorkerTaskPrompt(request string, activeTaskPath string) string {
	prompt := newTaskPrompt(request, activeTaskPath)
	if !w.config.RepoSearch {
		return prompt
	}
	timer := w.newRepoSearchTimer()
	block, outcome, results := routeWorkerRepoSearch(context.Background(), w.config.RepoRoot, request, timer.run)
	w.recordRepoSearchOutcome(repoSearchPhase, state.WorkerRole, 1, outcome, results, timer.elapsed)
	if block == "" {
		return prompt
	}
	return strings.TrimRight(prompt, "\n") + block
}

func routeWorkerRepoSearch(ctx context.Context, repoRoot string, request string, search repoSearchFunc) (string, string, []reposearch.Result) {
	if requestHasKnownRepoTarget(repoRoot, request) {
		return "", repoSearchKnownSkip, nil
	}
	report, err := search(ctx, repoRoot, request, reposearch.Options{MaxResults: RepoSearchMaxResults})
	if err != nil {
		return "", repoSearchErrorFallback, nil
	}
	if len(report.Results) == 0 {
		return "", repoSearchEmptyFallback, nil
	}
	return renderRepoSearchNavigation(report.Results), repoSearchHit, report.Results
}

func requestHasKnownRepoTarget(repoRoot string, request string) bool {
	for _, raw := range strings.Fields(request) {
		candidate := normalizeRequestPathCandidate(raw)
		if candidate == "" || filepath.IsAbs(candidate) {
			continue
		}
		path := filepath.Join(repoRoot, filepath.FromSlash(candidate))
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if strings.Contains(candidate, "/") || info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func normalizeRequestPathCandidate(raw string) string {
	value := strings.Trim(raw, "`'\"()[]{}<>.,;:、。")
	value = strings.TrimPrefix(value, "./")
	if index := strings.LastIndex(value, ":"); index > 0 {
		if _, err := strconv.Atoi(value[index+1:]); err == nil {
			value = value[:index]
		}
	}
	return filepath.ToSlash(value)
}

func renderRepoSearchNavigation(results []reposearch.Result) string {
	var block strings.Builder
	block.WriteString("\n\nREPO_SEARCH_NAVIGATION:\n")
	block.WriteString("MODE: wrapper-bm25-navigation\n")
	block.WriteString(fmt.Sprintf("RESULT_COUNT: %d\n", len(results)))
	for _, result := range results {
		block.WriteString("CANDIDATE: ")
		block.WriteString(result.Path)
		if result.Line > 0 {
			block.WriteString(":")
			block.WriteString(strconv.Itoa(result.Line))
		}
		block.WriteString("\n")
	}
	block.WriteString("AUTHORITY: navigation-only\n")
	block.WriteString("END_REPO_SEARCH_NAVIGATION\n")
	return block.String()
}

func (w *Workflow) recordRepoSearchOutcome(phase string, role state.SessionRole, seq int, outcome string, results []reposearch.Result, duration time.Duration) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return
	}
	now := time.Now
	if w.now != nil {
		now = w.now
	}
	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	if err := w.state.AppendTaskEvent(state.TaskEventRecord{
		TaskID:      taskID,
		Role:        string(role),
		Phase:       phase,
		Seq:         seq,
		Timestamp:   now().UTC(),
		Kind:        state.RepoSearchEventKind,
		Subtype:     outcome,
		SearchPaths: paths,
		DurationMS:  duration.Milliseconds(),
	}); err != nil {
		state.WarnTaskEventSkip("repo-search route追記", err)
	}
	w.state.RecordRepoSearchOutcome(phase, outcome, len(results), duration)
}
