package parentevidence

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestUntrackedSymbolTargetProvenByDeliveredSource(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "new.go"), []byte("package review\nfunc NewAPI() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openReviewCoverageBinding(t, repoRoot, st, "new.go:NewAPI")

	if err := PrintReviewEvidence(repoRoot, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if binding.Proof == nil || len(binding.Proof.Claims) != 1 || binding.Proof.Claims[0].Kind != "source" {
		t.Fatalf("untracked symbol target remains unproven after canonical source projection: %#v", binding.Proof)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("untracked symbol accept readiness = %v err=%v", ready, err)
	}
}

func TestDeletedNumericTargetProvenByDeletionDiff(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.Remove(filepath.Join(repoRoot, "review.go")); err != nil {
		t.Fatal(err)
	}
	openReviewCoverageBinding(t, repoRoot, st, "review.go:2")

	if err := PrintReviewEvidence(repoRoot, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	projectReviewCoverageManifest(t, repoRoot, st, Manifest{
		Version: ManifestVersion,
		Reason:  "inspect deleted line",
		Diff: []DiffRequest{{
			Question: "review deletion", Paths: []string{"review.go"}, BudgetBytes: 4096,
		}},
	})

	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if binding.Proof == nil || len(binding.Proof.Claims) != 1 || binding.Proof.Claims[0].Kind != "diff" {
		t.Fatalf("deleted numeric target remains unproven after delivered deletion diff: %#v", binding.Proof)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("deleted numeric accept readiness = %v err=%v", ready, err)
	}
}

func TestDeletedNumericTargetOutsideDeletionRangeRejectedBeforeBinding(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.Remove(filepath.Join(repoRoot, "review.go")); err != nil {
		t.Fatal(err)
	}
	err := recordReviewCoverageBinding(repoRoot, st, "review.go:5")
	if err == nil || !strings.Contains(err.Error(), "[deleted-line-out-of-range]") {
		t.Fatalf("out-of-range deleted target error = %v", err)
	}
	binding, bindingErr := st.CurrentParentReviewBinding()
	if bindingErr != nil {
		t.Fatal(bindingErr)
	}
	if binding != nil {
		t.Fatalf("out-of-range deleted target persisted binding: %#v", binding)
	}
}

func TestSourceSymbolCoverageRequiresCanonicalDeclarationSite(t *testing.T) {
	declared := SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\nfunc NewAPI() {}\n"}
	if !reviewSourceCoversTarget("new.go:NewAPI", declared) {
		t.Fatal("declared symbol in delivered source was not proven")
	}
	partialFunction := SourceBody{Path: "new.go", LineStart: 3, LineEnd: 3, Content: "func NewAPI() {}\n"}
	if !reviewSourceCoversTarget("new.go:NewAPI", partialFunction) {
		t.Fatal("canonical function fragment without package clause was not proven")
	}
	unproven := []struct {
		name   string
		source SourceBody
		target string
	}{
		{name: "comment only", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 3, Content: "package review\n// NewAPI placeholder\nvar Other = 1\n"}, target: "new.go:NewAPI"},
		{name: "string literal only", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\nvar Doc = \"NewAPI\"\n"}, target: "new.go:NewAPI"},
		{name: "reference only in another body", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\nfunc Caller() { NewAPI() }\n"}, target: "new.go:NewAPI"},
		{name: "same-named local", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 5, Content: "package review\nfunc Caller() {\n\tNewAPI := 1\n\t_ = NewAPI\n}\n"}, target: "new.go:NewAPI"},
		{name: "same-named unrelated field", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\ntype Holder struct{ NewAPI int }\n"}, target: "new.go:NewAPI"},
		{name: "same-named unrelated method", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 3, Content: "package review\ntype Holder struct{}\nfunc (Holder) NewAPI() {}\n"}, target: "new.go:NewAPI"},
		{name: "identifier superstring", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\nfunc NewAPIOnly() {}\n"}, target: "new.go:NewAPI"},
		{name: "identifier substring", source: declared, target: "new.go:New"},
		{name: "missing symbol", source: declared, target: "new.go:MissingAPI"},
		{name: "compound locator", source: declared, target: "new.go:NewAPI,Other"},
		{name: "non-go file", source: SourceBody{Path: "notes.md", LineStart: 1, LineEnd: 2, Content: "package review\nfunc NewAPI() {}\n"}, target: "notes.md:NewAPI"},
		{name: "whole-file diff locator", source: SourceBody{Path: "new.go", LineStart: 1, LineEnd: 2, Content: "package review\nvar Doc = \"@diff\"\n"}, target: "new.go:" + reviewtarget.WholeFileDiffLocator},
	}
	for _, tc := range unproven {
		if reviewSourceCoversTarget(tc.target, tc.source) {
			t.Fatalf("%s counted as source proof: %q", tc.name, tc.target)
		}
	}
}

func TestSourceSymbolCoverageAcceptsCanonicalGoDeclarationKinds(t *testing.T) {
	cases := []struct {
		kind    string
		target  string
		content string
	}{
		{kind: "function", target: "new.go:NewAPI", content: "package review\nfunc NewAPI() {}\n"},
		{kind: "method", target: "new.go:Projector.NewAPI", content: "package review\ntype Projector struct{}\nfunc (p *Projector) NewAPI() {}\n"},
		{kind: "type", target: "new.go:NewAPI", content: "package review\ntype NewAPI struct{}\n"},
		{kind: "var", target: "new.go:NewAPI", content: "package review\nvar NewAPI = 1\n"},
		{kind: "const", target: "new.go:NewAPI", content: "package review\nconst NewAPI = 1\n"},
		{kind: "struct field", target: "new.go:Holder.NewAPI", content: "package review\ntype Holder struct{ NewAPI int }\n"},
		{kind: "interface method", target: "new.go:Runner.NewAPI", content: "package review\ntype Runner interface{ NewAPI() }\n"},
	}
	for _, tc := range cases {
		source := SourceBody{Path: "new.go", LineStart: 1, LineEnd: 3, Content: tc.content}
		if !reviewSourceCoversTarget(tc.target, source) {
			t.Fatalf("%s declaration did not prove %s", tc.kind, tc.target)
		}
	}
}

func newReviewCoverageStore(t *testing.T) (string, *state.StateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	gitReviewCoverageCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 1\nvar third = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repoRoot, "add", "review.go")
	gitReviewCoverageCommand(t, repoRoot, "-c", "user.name=review coverage test", "-c", "user.email=review-coverage@example.invalid", "commit", "-q", "-m", "seed")

	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "review-target-coverage", RepoRoot: repoRoot}
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
	return repoRoot, st
}

func openReviewCoverageBinding(t *testing.T, repoRoot string, st *state.StateStore, target string) {
	t.Helper()
	if err := recordReviewCoverageBinding(repoRoot, st, target); err != nil {
		t.Fatal(err)
	}
}

func recordReviewCoverageBinding(repoRoot string, st *state.StateStore, target string) error {
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return err
	}
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the current target and decide whether to accept.",
		Targets:     []string{target},
	}
	digest := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	return st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, digest)
}

func projectReviewCoverageManifest(t *testing.T, repoRoot string, st *state.StateStore, manifest Manifest) {
	t.Helper()
	ownerCallID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, ownerCallID, Providers{})
	projector.Project(manifest)
	if err := projector.Commit(&bytes.Buffer{}, manifest.Reason); err != nil {
		t.Fatal(err)
	}
}

func gitReviewCoverageCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
