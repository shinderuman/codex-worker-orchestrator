package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

func TestReviewedBoundaryContextMarksOnlyIdenticalBlobsReviewed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "reviewed-keep.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "conflict-merge.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, repo, "add", "reviewed-keep.md", "conflict-merge.md")
	runRetentionGit(t, repo, "commit", "-q", "-m", "boundary files")
	stateBase := t.TempDir()
	st, err := state.NewStateStore(config.AppConfig{StateBase: stateBase, RepoHash: "boundaryhash", RepoRoot: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(config.AppConfig{RepoRoot: repo}, st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "reviewed-keep.md"), []byte("reviewed body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "conflict-merge.md"), []byte("merged conflict body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(config.AppConfig{RepoRoot: repo, StateBase: stateBase, RepoHash: "boundaryhash"}, st, nil, nil)

	first := w.reviewedBoundaryContext(repo, 0)
	if !strings.Contains(first, "reviewed-keep.md: new-boundary") || !strings.Contains(first, "conflict-merge.md: new-boundary") {
		t.Fatalf("first round boundary = %q", first)
	}

	if err := os.WriteFile(filepath.Join(repo, "conflict-merge.md"), []byte("changed by fix round\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := w.reviewedBoundaryContext(repo, 1)
	if !strings.Contains(second, "reviewed-keep.md: reviewed(round 0)") {
		t.Fatalf("identical blob was not classified as reviewed: %q", second)
	}
	if !strings.Contains(second, "conflict-merge.md: new-boundary") {
		t.Fatalf("changed blob was not classified as new boundary: %q", second)
	}
	if !strings.Contains(second, "NEW_BOUNDARY_COUNT: 1") {
		t.Fatalf("boundary count missing: %q", second)
	}

	rounds := reviewedBoundaryForState(st)
	if len(rounds) != 1 || rounds[0].ReviewNumber != 0 {
		t.Fatalf("reviewed ledger rounds = %#v", rounds)
	}
	identities, err := taskdiff.FileIdentities(repo, []string{"reviewed-keep.md"})
	if err != nil {
		t.Fatal(err)
	}
	if identities[0].HeadDigest == "" || identities[0].WorktreeDigest == "" {
		t.Fatalf("identities = %#v", identities[0])
	}
}

func TestReviewedBoundaryFailsClosedWithoutProvenance(t *testing.T) {
	repo := newRetentionGitRepo(t)
	stateBase := t.TempDir()
	st, err := state.NewStateStore(config.AppConfig{StateBase: stateBase, RepoHash: "boundaryhash2", RepoRoot: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(config.AppConfig{RepoRoot: repo}, st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "unprovenanced.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(config.AppConfig{RepoRoot: repo, StateBase: stateBase, RepoHash: "boundaryhash2"}, st, nil, nil)

	boundary := w.reviewedBoundaryContext(repo, 0)
	if !strings.Contains(boundary, "unprovenanced.md: new-boundary") {
		t.Fatalf("unprovenanced file must stay in the review target: %q", boundary)
	}
}
