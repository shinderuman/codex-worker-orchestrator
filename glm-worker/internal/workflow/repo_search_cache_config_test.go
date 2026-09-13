package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

func TestRepoSearchTimerInjectsConfiguredCacheRoot(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "search")
	now := time.Unix(1_700_000_000, 0).UTC()
	w := &Workflow{
		config: config.AppConfig{RepoSearchCacheRoot: cacheRoot},
		now:    func() time.Time { return now },
	}
	w.repoSearch = func(_ context.Context, root string, query string, opts reposearch.Options) (reposearch.Report, error) {
		if root != "/repo" || query != "needle" {
			t.Fatalf("root=%q query=%q", root, query)
		}
		if opts.CacheRoot != cacheRoot {
			t.Fatalf("CacheRoot = %q, want %q", opts.CacheRoot, cacheRoot)
		}
		if opts.MaxResults != RepoSearchMaxResults {
			t.Fatalf("MaxResults = %d, want %d", opts.MaxResults, RepoSearchMaxResults)
		}
		return reposearch.Report{}, nil
	}

	timer := w.newRepoSearchTimer()
	if _, err := timer.run(context.Background(), "/repo", "needle", reposearch.Options{MaxResults: RepoSearchMaxResults}); err != nil {
		t.Fatal(err)
	}
}
