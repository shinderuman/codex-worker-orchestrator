package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteAcceptRequiresProjectedReviewEvidence(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2")

	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "requires matching current target evidence") {
		t.Fatalf("accept without projected evidence = %v", err)
	}

	manifestPath := writeParentReviewEvidenceManifest(t, parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "inspect exact review target",
		Source: []parentEvidenceSourceRequest{{
			Question:    "inspect target",
			Path:        "review.go",
			LineStart:   1,
			LineEnd:     3,
			BudgetBytes: 4096,
		}},
	})
	if err := printParentEvidence(Command{EvidenceManifest: manifestPath}, cfg, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output acceptOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Accepted {
		t.Fatalf("accept after projected evidence = %#v", output)
	}
}

func TestParentEvidenceMatchingSourceEnablesReviewAccept(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2")

	manifestPath := writeParentReviewEvidenceManifest(t, parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "inspect exact review target",
		Source: []parentEvidenceSourceRequest{{
			Question:    "inspect target",
			Path:        "review.go",
			LineStart:   1,
			LineEnd:     3,
			BudgetBytes: 4096,
		}},
	})
	var stdout bytes.Buffer
	if err := printParentEvidence(Command{EvidenceManifest: manifestPath}, cfg, st, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("matching evidence produced no model-visible output")
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil || binding.Proof == nil {
		t.Fatalf("review proof = %#v err=%v", binding, err)
	}
	if len(binding.Proof.Claims) != 1 || binding.Proof.Claims[0].Kind != "source" {
		t.Fatalf("review proof claims = %#v", binding.Proof.Claims)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept after matching evidence = %v err=%v", accepted, err)
	}
}

func TestParentEvidenceUnrelatedSourceDoesNotProveReview(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2")

	manifestPath := writeParentReviewEvidenceManifest(t, parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "inspect unrelated source",
		Source: []parentEvidenceSourceRequest{{
			Question:    "unrelated",
			Path:        "other.go",
			LineStart:   1,
			LineEnd:     2,
			BudgetBytes: 4096,
		}},
	})
	if err := printParentEvidence(Command{EvidenceManifest: manifestPath}, cfg, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if binding.Proof != nil {
		t.Fatalf("unrelated source created proof: %#v", binding.Proof)
	}
	if accepted, err := st.AcceptParentReview(); err == nil || accepted {
		t.Fatalf("accept after unrelated evidence = %v err=%v", accepted, err)
	}
}

func TestParentEvidenceSourceMustCoverTargetRange(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:3")

	manifestPath := writeParentReviewEvidenceManifest(t, parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "inspect too narrow source",
		Source: []parentEvidenceSourceRequest{{
			Question:    "too narrow",
			Path:        "review.go",
			LineStart:   1,
			LineEnd:     2,
			BudgetBytes: 4096,
		}},
	})
	if err := printParentEvidence(Command{EvidenceManifest: manifestPath}, cfg, st, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if binding.Proof != nil {
		t.Fatalf("narrow source created proof: %#v", binding.Proof)
	}
}

func TestParentEvidenceSnapshotChangeCannotCreateProof(t *testing.T) {
	cfg, st, snapshot := newParentEvidenceReviewStore(t)
	openParentEvidenceReview(t, st, snapshot, "review.go:2")
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "review.go"), []byte("package review\nvar target = 2\nvar changed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifestPath := writeParentReviewEvidenceManifest(t, parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "stale snapshot",
		Source: []parentEvidenceSourceRequest{{
			Question:    "stale",
			Path:        "review.go",
			LineStart:   1,
			LineEnd:     3,
			BudgetBytes: 4096,
		}},
	})
	if err := printParentEvidence(Command{EvidenceManifest: manifestPath}, cfg, st, &bytes.Buffer{}); err == nil {
		t.Fatal("changed snapshot returned successful review evidence")
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if binding.Proof != nil {
		t.Fatalf("changed snapshot created proof: %#v", binding.Proof)
	}
}

func TestParentReviewEvidenceClaimCoverage(t *testing.T) {
	parts := []parentEvidencePart{
		{
			Kind: "source", Digest: "source-digest", Locator: "review.go:8-15",
			Source: &parentEvidenceSourceBody{Path: "review.go", LineStart: 8, LineEnd: 15, Content: "body"},
		},
		{
			Kind: "diff", Digest: "diff-digest", Locator: "git diff HEAD -- symbol.go",
			Diff: &parentEvidenceDiffBody{
				Paths: []string{"symbol.go"}, Body: "diff body",
				Files: []parentEvidenceDiffFile{{Path: "symbol.go", Status: "M", HeadBlob: "blob", WorktreeSHA: "sha"}},
			},
		},
	}
	claims, ok := parentReviewEvidenceClaims([]string{"review.go:10-12", "symbol.go:TargetSymbol"}, parts)
	if !ok || len(claims) != 2 || claims[0].Kind != "source" || claims[1].Kind != "diff" {
		t.Fatalf("claims = %#v complete=%v", claims, ok)
	}
	if _, ok := parentReviewEvidenceClaims([]string{"review.go:16"}, parts); ok {
		t.Fatal("source outside requested target range counted as proof")
	}
	parts[0].Source.Content = ""
	if _, ok := parentReviewEvidenceClaims([]string{"review.go:10"}, parts); ok {
		t.Fatal("non-model-visible source counted as proof")
	}
}

func newParentEvidenceReviewStore(t *testing.T) (config.AppConfig, *state.StateStore, state.SnapshotDigest) {
	t.Helper()
	repoRoot := t.TempDir()
	gitParentReviewEvidenceCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 1\nvar third = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "other.go"), []byte("package review\nvar other = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitParentReviewEvidenceCommand(t, repoRoot, "add", "review.go", "other.go")
	gitParentReviewEvidenceCommand(t, repoRoot, "-c", "user.name=review evidence test", "-c", "user.email=review-evidence@example.invalid", "commit", "-q", "-m", "seed")

	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "review-evidence", RepoRoot: repoRoot}
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
	return cfg, st, state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func openParentEvidenceReview(t *testing.T, st *state.StateStore, snapshot state.SnapshotDigest, target string) {
	t.Helper()
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the current target and decide whether to accept.",
		Targets:     []string{target},
	}
	if err := st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, snapshot); err != nil {
		t.Fatal(err)
	}
}

func writeParentReviewEvidenceManifest(t *testing.T, manifest parentEvidenceManifest) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitParentReviewEvidenceCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
