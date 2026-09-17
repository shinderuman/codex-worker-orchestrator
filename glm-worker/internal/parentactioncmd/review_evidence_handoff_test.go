package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewEvidenceTerminalEnvelopeCarriesPostActionHandoff(t *testing.T) {
	repoRoot := t.TempDir()
	gitReviewEvidenceCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 1\nvar third = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewEvidenceCommand(t, repoRoot, "add", "review.go")
	gitReviewEvidenceCommand(t, repoRoot, "-c", "user.name=review evidence test", "-c", "user.email=review-evidence@example.invalid", "commit", "-q", "-m", "seed")

	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "parent-review-evidence-handoff", RepoRoot: repoRoot}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the current target and decide whether to accept.",
		Targets:     []string{"review.go:2-3(exact target)"},
	}
	if err := st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, state.SnapshotDigest{
		Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest,
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allows(state.ParentActionAccept) {
		t.Fatalf("pre-evidence action plan unexpectedly allows accept: %#v", plan)
	}

	var stdout bytes.Buffer
	if err := executeWithTerminalEnvelope(cfg, []string{actionReviewEvidence}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("review-evidence envelope is not JSON: %v\n%s", err, stdout.String())
	}
	if envelope.Status != "parent_action_terminal" || len(envelope.Terminal) == 0 || len(envelope.Handoff) == 0 {
		t.Fatalf("review-evidence envelope = %#v", envelope)
	}
	var handoff struct {
		Consistent     bool     `json:"consistent"`
		RequiredAction *string  `json:"required_action"`
		AllowedActions []string `json:"allowed_actions"`
	}
	if err := json.Unmarshal(envelope.Handoff, &handoff); err != nil {
		t.Fatal(err)
	}
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("post-evidence handoff = %#v", handoff)
	}
	if !containsReviewEvidenceAction(handoff.AllowedActions, string(state.ParentActionAccept)) {
		t.Fatalf("post-evidence handoff does not advertise accept: %#v", handoff.AllowedActions)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("review accept readiness = %v err=%v", ready, err)
	}
}

func containsReviewEvidenceAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}
