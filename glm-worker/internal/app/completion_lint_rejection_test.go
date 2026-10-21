package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestCompletionLintFailureDoesNotPublishOrRetire(t *testing.T) {
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remote := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef)
	fail := filepath.Join(os.Getenv("QUALITY_TOOLS_BIN_DIR"), "fail")
	if err := os.WriteFile(fail, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err == nil || !strings.Contains(err.Error(), "completion final Metadata lint failed") {
		t.Fatalf("publication did not reject final commit lint: %v", err)
	}
	after, err := store.LoadHead()
	if err != nil || after.IntegrationTip != before.IntegrationTip || !reflect.DeepEqual(after.MetadataLineageRef, before.MetadataLineageRef) {
		t.Fatalf("lint failure retired or changed integration authority: %v", err)
	}
	if got := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); string(got) != string(remote) {
		t.Fatal("lint failure advanced the remote")
	}
}
