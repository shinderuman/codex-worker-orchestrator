package app

import (
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestMissingCommittedControllerAuthorityDoesNotFallBackToStateStore(t *testing.T) {
	repo := initGitRepo(t)
	writeControllerAdmissionFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeControllerAdmissionFile(t, repo, "source.txt", "baseline\n")
	commitControllerAdmissionRepo(t, repo)

	hash := config.RepoHashFor(repo)
	cfg := config.AppConfig{
		StateBase:             t.TempDir(),
		RepoHash:              hash,
		RepoRoot:              repo,
		RepoShort:             hash[:12],
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		CodexConfigDir:        t.TempDir(),
	}
	legacy := state.AttachStateStore(cfg)
	if err := legacy.Write("active-task", "IMPLEMENTATION_TASKS/legacy-only.md"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	err := Execute(Command{Mode: ModeNewTask, Payload: "must not use legacy authority"}, cfg, runner.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read committed implementation plan") {
		t.Fatalf("missing committed authority did not fail closed: %v", err)
	}
	if len(runner.prompts) != 0 {
		t.Fatalf("missing controller authority fell through to model dispatch: prompts=%d", len(runner.prompts))
	}
	if got := legacy.ReadOr("active-task", ""); got != "IMPLEMENTATION_TASKS/legacy-only.md" {
		t.Fatalf("failed admission rewrote legacy state: %q", got)
	}
}
