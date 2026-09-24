package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestLoadReviewedBlobRoundsIgnoresNonCurrentVersions(t *testing.T) {
	w := newReviewedBoundarySchemaWorkflow(t, "ledger-version")
	ledger := strings.Join([]string{
		`{"version":1,"review_number":1,"files":[]}`,
		`{"review_number":2,"files":[]}`,
		`{"version":0,"review_number":3,"files":[]}`,
		`{"version":999,"review_number":4,"files":{}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(w.state.Path(reviewedBlobsFile), []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}

	rounds, err := w.loadReviewedBlobRounds()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || rounds[0].Version != reviewedBlobsVersion || rounds[0].ReviewNumber != 1 {
		t.Fatalf("reviewed rounds = %#v", rounds)
	}
}

func TestPromoteLastReviewBlobsRequiresCurrentVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "missing", version: "", want: false},
		{name: "zero", version: `"version":0,`, want: false},
		{name: "future", version: `"version":999,`, want: false},
		{name: "current", version: `"version":1,`, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newReviewedBoundarySchemaWorkflow(t, "promotion-"+tc.name)
			record := "{" + tc.version + `"review_number":1,"files":[{"path":"tracked.txt","head_digest":"h","index_digest":"i","worktree_digest":"w"}]}`
			if err := os.WriteFile(w.state.Path(lastReviewBlobsFile), []byte(record), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := w.promoteLastReviewBlobs(2); err != nil {
				t.Fatal(err)
			}
			rounds, err := w.loadReviewedBlobRounds()
			if err != nil {
				t.Fatal(err)
			}
			if tc.want {
				if len(rounds) != 1 || rounds[0].ReviewNumber != 1 {
					t.Fatalf("promoted rounds = %#v", rounds)
				}
				return
			}
			if len(rounds) != 0 {
				t.Fatalf("non-current round was promoted: %#v", rounds)
			}
		})
	}
}

func TestPromoteLastReviewBlobsSkipsFutureIncompatibleShape(t *testing.T) {
	w := newReviewedBoundarySchemaWorkflow(t, "promotion-future-shape")
	record := `{"version":999,"review_number":1,"files":{"future":"shape"}}`
	if err := os.WriteFile(w.state.Path(lastReviewBlobsFile), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.promoteLastReviewBlobs(2); err != nil {
		t.Fatal(err)
	}
	rounds, err := w.loadReviewedBlobRounds()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 0 {
		t.Fatalf("future incompatible round was promoted: %#v", rounds)
	}
}

func newReviewedBoundarySchemaWorkflow(t *testing.T, repoHash string) *Workflow {
	t.Helper()
	repo := newRetentionGitRepo(t)
	stateBase := t.TempDir()
	cfg := config.AppConfig{StateBase: stateBase, RepoHash: repoHash, RepoRoot: repo}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return NewWorkflow(cfg, st, nil, nil)
}
