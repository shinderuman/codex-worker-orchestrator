package parentactioncmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestSameTaskScopeAdmitsMachineBoundCurrentDiffFinding(t *testing.T) {
	cfg, st, _, repo := newSameTaskScopeFixture(t)
	writeSameTaskScopeFile(t, filepath.Join(repo, "code.go"), "package scope\nvar Current = 2\n")
	bindSameTaskScopeReview(t, cfg, st, "code.go:2")

	if err := validateSameTaskFixAdmission(cfg, []string{"--origin", state.ParentOriginGLMReviewer}); err != nil {
		t.Fatalf("current task diff finding rejected: %v", err)
	}
}

func TestSameTaskScopeAdmitsMachineBoundContractFinding(t *testing.T) {
	cfg, st, taskPath, repo := newSameTaskScopeFixture(t)
	line := sameTaskScopeLineContaining(t, filepath.Join(repo, filepath.FromSlash(taskPath)), "machine-bound contract correction")
	bindSameTaskScopeReview(t, cfg, st, taskPath+":"+strconv.Itoa(line))

	if err := validateSameTaskFixAdmission(cfg, []string{"--origin", state.ParentOriginGLMReviewer}); err != nil {
		t.Fatalf("machine-bound Contract finding rejected: %v", err)
	}
}

func TestSameTaskScopeRejectsIndependentReviewTarget(t *testing.T) {
	cfg, st, _, repo := newSameTaskScopeFixture(t)
	writeSameTaskScopeFile(t, filepath.Join(repo, "code.go"), "package scope\nvar Current = 2\n")
	bindSameTaskScopeReview(t, cfg, st, "external.go:2")

	err := validateSameTaskFixAdmission(cfg, []string{"--origin", state.ParentOriginGLMReviewer})
	if err == nil || !strings.Contains(err.Error(), sameTaskScopeRegistrationHint) {
		t.Fatalf("independent review target error = %v", err)
	}
}

func TestSameTaskScopeRejectsCallerSelectedTaskLocator(t *testing.T) {
	cfg, _, taskPath, repo := newSameTaskScopeFixture(t)
	line := sameTaskScopeLineContaining(t, filepath.Join(repo, filepath.FromSlash(taskPath)), "machine-bound contract correction")

	err := validateSameTaskFixAdmission(cfg, []string{"--task-locator", taskPath + ":" + strconv.Itoa(line)})
	if err == nil || !strings.Contains(err.Error(), "invalid fix options") {
		t.Fatalf("caller-selected Task locator error = %v", err)
	}
}

func TestSameTaskScopeRejectsStaleMachineReviewProof(t *testing.T) {
	cfg, st, _, repo := newSameTaskScopeFixture(t)
	writeSameTaskScopeFile(t, filepath.Join(repo, "code.go"), "package scope\nvar Current = 2\n")
	bindSameTaskScopeReview(t, cfg, st, "code.go:2")
	writeSameTaskScopeFile(t, filepath.Join(repo, "code.go"), "package scope\nvar Current = 3\n")

	err := validateSameTaskFixAdmission(cfg, []string{"--origin", state.ParentOriginGLMReviewer})
	if err == nil || !strings.Contains(err.Error(), sameTaskScopeRegistrationHint) {
		t.Fatalf("stale review proof error = %v", err)
	}
}

func newSameTaskScopeFixture(t *testing.T) (config.AppConfig, *state.StateStore, string, string) {
	t.Helper()
	repo := t.TempDir()
	gitReviewEvidenceCommand(t, repo, "init", "-q")
	gitReviewEvidenceCommand(t, repo, "config", "user.email", "same-task@example.invalid")
	gitReviewEvidenceCommand(t, repo, "config", "user.name", "Same Task Scope Test")

	taskPath := "IMPLEMENTATION_TASKS/active.md"
	writeSameTaskScopeFile(t, filepath.Join(repo, filepath.FromSlash(taskPath)), sameTaskScopeTask())
	writeSameTaskScopeFile(t, filepath.Join(repo, "code.go"), "package scope\nvar Current = 1\n")
	writeSameTaskScopeFile(t, filepath.Join(repo, "external.go"), "package scope\nvar External = 1\n")
	gitReviewEvidenceCommand(t, repo, "add", ".")
	gitReviewEvidenceCommand(t, repo, "commit", "-q", "-m", "baseline")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "same-task-scope"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", taskPath); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	return cfg, st, taskPath, repo
}

func bindSameTaskScopeReview(t *testing.T, cfg config.AppConfig, st *state.StateStore, target string) {
	t.Helper()
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Does this concrete finding require a same-task correction?",
		Targets:     []string{target},
	}
	if err := st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, state.SnapshotDigest{
		Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest,
	}); err != nil {
		t.Fatal(err)
	}
	var evidence bytes.Buffer
	if err := executeParentReviewEvidence(cfg, []string{actionReviewEvidence}, &evidence); err != nil {
		t.Fatalf("project review evidence: %v", err)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("review evidence ready=%v err=%v output=%s", ready, err, evidence.String())
	}
}

func sameTaskScopeTask() string {
	return "# Task: active\n\n" +
		"## Original instruction\n\nfixture\n\n" +
		"## Amendments\n\nnone\n\n" +
		"## Purpose\n\nfixture\n\n" +
		"## External feasibility\n\nstatus: not-applicable\n\n" +
		"## Contract\n\n- machine-bound contract correction\n\n" +
		"## Must not\n\n- do not widen scope\n\n" +
		"## Acceptance criteria\n\n- machine-bound acceptance correction\n\n" +
		"## Historical invariants\n\nfixture\n\n" +
		"## Dependencies\n\nnone\n"
}

func sameTaskScopeLineContaining(t *testing.T, path, needle string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for index, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			return index + 1
		}
	}
	t.Fatalf("%q not found in %s", needle, path)
	return 0
}

func writeSameTaskScopeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

var _ io.Writer = (*bytes.Buffer)(nil)
