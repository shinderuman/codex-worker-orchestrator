package workflow

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityEvidenceAdditionsAndMechanicalRenameStayLow(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceGoTest("got", "1", true))
	writeGitTestFile(t, root, "sample_test.go", qualityEvidenceGoTest("actual", "1", true)+qualityEvidenceSecondGoTest())
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if decision.High {
		t.Fatalf("additive/mechanical change raised risk: %#v", decision)
	}
}

func TestQualityEvidenceAssertionRemovalRaisesRisk(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceGoTest("got", "1", true))
	writeGitTestFile(t, root, "sample_test.go", qualityEvidenceGoTest("got", "1", false))
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if !decision.High || decision.Source != "track-a-evidence-removed" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestQualityEvidenceExpectedBehaviorChangeRaisesRisk(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceGoTest("got", "1", true))
	writeGitTestFile(t, root, "sample_test.go", qualityEvidenceGoTest("got", "2", true))
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if !decision.High {
		t.Fatalf("expected behavior change stayed LOW: %#v", decision)
	}
}

func TestQualityEvidenceNegativeCaseDeletionRaisesRisk(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceSubtests(true))
	writeGitTestFile(t, root, "sample_test.go", qualityEvidenceSubtests(false))
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if !decision.High {
		t.Fatalf("negative case deletion stayed LOW: %#v", decision)
	}
}

func TestQualityEvidenceFileRenameWithSameEvidenceStaysLow(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceGoTest("got", "1", true))
	if err := os.Rename(filepath.Join(root, "sample_test.go"), filepath.Join(root, "renamed_test.go")); err != nil {
		t.Fatal(err)
	}
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if decision.High {
		t.Fatalf("same-evidence rename raised risk: %#v", decision)
	}
}

func TestParentBehaviorEvalStatusOnlyChangeStaysLow(t *testing.T) {
	root, baseline := newQualityEvidenceRegistryRepo(t, "not-run", "positive contract")
	writeGitTestFile(t, root, parentBehaviorEvalPath, qualityEvidenceRegistry("pass", "positive contract"))
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if decision.High {
		t.Fatalf("status-only change raised risk: %#v", decision)
	}
}

func TestParentBehaviorEvalContractChangeRaisesRisk(t *testing.T) {
	root, baseline := newQualityEvidenceRegistryRepo(t, "not-run", "positive contract")
	writeGitTestFile(t, root, parentBehaviorEvalPath, qualityEvidenceRegistry("not-run", "weakened contract"))
	decision := qualityEvidenceDecisionForTest(t, root, baseline)
	if !decision.High || decision.Source != "track-b-live-eval-contract-changed" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestEffectiveRiskIncludesQualityEvidenceWeakening(t *testing.T) {
	root, baseline := newQualityEvidenceRepo(t, qualityEvidenceGoTest("got", "1", true))
	writeGitTestFile(t, root, "sample_test.go", qualityEvidenceGoTest("got", "1", false))
	cfg := config.AppConfig{RepoRoot: root, StateBase: t.TempDir(), RepoHash: "quality-evidence-risk"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-head", baseline); err != nil {
		t.Fatal(err)
	}
	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	risk := workflow.computeEffectiveRisk(packet.Result{Risk: packet.RiskLow}, 0, false, false)
	if !risk.high || !strings.Contains(risk.source, "quality-evidence:track-a-evidence-removed") {
		t.Fatalf("risk = %#v", risk)
	}
}

func newQualityEvidenceRepo(t *testing.T, testContent string) (string, string) {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "quality@example.invalid")
	runGitTest(t, root, "config", "user.name", "quality evidence")
	writeGitTestFile(t, root, "sample_test.go", testContent)
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "baseline")
	return root, runGitTest(t, root, "rev-parse", "HEAD")
}

func newQualityEvidenceRegistryRepo(t *testing.T, status, positive string) (string, string) {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "quality@example.invalid")
	runGitTest(t, root, "config", "user.name", "quality evidence")
	writeGitTestFile(t, root, parentBehaviorEvalPath, qualityEvidenceRegistry(status, positive))
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "baseline")
	return root, runGitTest(t, root, "rev-parse", "HEAD")
}

func qualityEvidenceDecisionForTest(t *testing.T, root, baseline string) qualityEvidenceDecision {
	t.Helper()
	paths, err := collectChangedPaths(root, baseline)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := classifyQualityEvidence(root, baseline, paths)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func qualityEvidenceGoTest(variable, expected string, assertion bool) string {
	body := ""
	if assertion {
		body = "\tif " + variable + " != " + expected + " {\n\t\tt.Fatalf(\"got %d\", " + variable + ")\n\t}\n"
	}
	return "package sample\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\t" + variable + " := 1\n" + body + "}\n"
}

func qualityEvidenceSecondGoTest() string {
	return "\nfunc TestSecond(t *testing.T) {\n\tif 2 != 2 {\n\t\tt.Fatal(\"second\")\n\t}\n}\n"
}

func qualityEvidenceSubtests(includeNegative bool) string {
	negative := ""
	if includeNegative {
		negative = "\tt.Run(\"negative\", func(t *testing.T) {\n\t\tif false {\n\t\t\tt.Fatal(\"negative\")\n\t\t}\n\t})\n"
	}
	return "package sample\n\nimport \"testing\"\n\nfunc TestCases(t *testing.T) {\n\tt.Run(\"positive\", func(t *testing.T) {\n\t\tif false {\n\t\t\tt.Fatal(\"positive\")\n\t\t}\n\t})\n" + negative + "}\n"
}

func qualityEvidenceRegistry(status, positive string) string {
	return `{"version":1,"cases":[{"id":"case-a","contract_sources":["contract.md"],"positive":"` + positive + `","negative":"negative contract","evidence":"evidence contract","run_policy":"explicit-user-authorization","status":"` + status + `"}]}`
}
