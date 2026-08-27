package workflow

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRouteWorkerRepoSearchSkipsKnownTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "workflow", "workflow.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package workflow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	search := func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		calls++
		return reposearch.Report{}, nil
	}
	block, outcome := routeWorkerRepoSearch(context.Background(), root, "fix `internal/workflow/workflow.go`", search)
	if calls != 0 || block != "" || outcome != repoSearchKnownSkip {
		t.Fatalf("calls=%d block=%q outcome=%q", calls, block, outcome)
	}
}

func TestRouteWorkerRepoSearchInjectsUnknownTargetCandidates(t *testing.T) {
	search := func(_ context.Context, root string, query string, opts reposearch.Options) (reposearch.Report, error) {
		if root != "/repo" || query != "find worker dispatch" || opts.MaxResults != repoSearchMaxResults {
			t.Fatalf("root=%q query=%q opts=%#v", root, query, opts)
		}
		return reposearch.Report{Results: []reposearch.Result{
			{Path: "glm-worker/internal/workflow/workflow.go", Line: 142},
			{Path: "glm-worker/internal/workflow/prompts.go", Line: 31},
		}}, nil
	}
	block, outcome := routeWorkerRepoSearch(context.Background(), "/repo", "find worker dispatch", search)
	if outcome != repoSearchHit {
		t.Fatalf("outcome=%q", outcome)
	}
	for _, want := range []string{
		"REPO_SEARCH_NAVIGATION:",
		"CANDIDATE: glm-worker/internal/workflow/workflow.go:142",
		"CANDIDATE: glm-worker/internal/workflow/prompts.go:31",
		"AUTHORITY: navigation-only",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("missing %q in %s", want, block)
		}
	}
}

func TestRouteWorkerRepoSearchFallsBackOnEmptyOrError(t *testing.T) {
	tests := []struct {
		name    string
		report  reposearch.Report
		err     error
		outcome string
	}{
		{name: "empty", outcome: repoSearchEmptyFallback},
		{name: "error", err: errors.New("unavailable"), outcome: repoSearchErrorFallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search := func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
				return tt.report, tt.err
			}
			block, outcome := routeWorkerRepoSearch(context.Background(), t.TempDir(), "unknown target", search)
			if block != "" || outcome != tt.outcome {
				t.Fatalf("block=%q outcome=%q", block, outcome)
			}
		})
	}
}

func TestNewWorkerTaskPromptRecordsRepoSearchTelemetry(t *testing.T) {
	root := t.TempDir()
	cfg := config.AppConfig{RepoRoot: root, RepoHash: "repo-search-routing", StateBase: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	w := NewWorkflow(cfg, st, nil, nil)
	w.now = func() time.Time { return fixed }
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		return reposearch.Report{Results: []reposearch.Result{{Path: "internal/worker.go", Line: 9}}}, nil
	}
	prompt := w.newWorkerTaskPrompt("unknown worker target", "")
	if !strings.Contains(prompt, "CANDIDATE: internal/worker.go:9") {
		t.Fatalf("navigation missing from production prompt: %s", prompt)
	}
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("repo-search event missing: %v", scanner.Err())
	}
	event, err := state.ParseTaskEventLine(scanner.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if scanner.Scan() {
		t.Fatalf("unexpected extra repo-search event: %q", scanner.Text())
	}
	if event.Phase != repoSearchPhase || event.Kind != "navigation" || event.Subtype != repoSearchHit {
		t.Fatalf("event=%#v", event)
	}
	if !event.Timestamp.Equal(fixed) || event.TaskID != taskID || event.Role != string(state.WorkerRole) {
		t.Fatalf("event=%#v", event)
	}
}
