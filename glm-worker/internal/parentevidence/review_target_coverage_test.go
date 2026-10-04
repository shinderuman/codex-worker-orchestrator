package parentevidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDeletedNumericTargetOutsideDeletionRangeIsNotProofAddressable(t *testing.T) {
	repoRoot, _ := newReviewCoverageStore(t)
	if err := os.Remove(filepath.Join(repoRoot, "review.go")); err != nil {
		t.Fatal(err)
	}
	target, err := reviewtarget.ParseTarget("review.go:5")
	if err != nil {
		t.Fatal(err)
	}
	err = reviewtarget.ValidateProofAddressable(repoRoot, target)
	if err == nil || !strings.Contains(err.Error(), "[deleted-line-out-of-range]") {
		t.Fatalf("out-of-range deleted target error = %v", err)
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
	partialVarGroup := SourceBody{Path: "new.go", LineStart: 8, LineEnd: 10, Content: "var (\n\tNewAPI = 1\n)\n"}
	if !reviewSourceCoversTarget("new.go:NewAPI", partialVarGroup) {
		t.Fatal("canonical var-group fragment without package clause was not proven")
	}
	partialMember := SourceBody{Path: "new.go", LineStart: 12, LineEnd: 12, Content: "type Holder struct{ NewAPI int }\n"}
	if !reviewSourceCoversTarget("new.go:Holder.NewAPI", partialMember) {
		t.Fatal("canonical member fragment without package clause was not proven")
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

func gitReviewCoverageCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
