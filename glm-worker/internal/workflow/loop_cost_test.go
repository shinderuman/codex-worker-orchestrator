package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopCostObservationContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)

	readContractFile := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}

	workerPrompt := readContractFile("codex/glm-worker/prompts/WORKER.md")
	reviewerPrompt := readContractFile("codex/glm-worker/prompts/REVIEWER.md")
	execution := readContractFile("codex/instructions/glm-execution.md")

	workerWires := []string{
		"## 反復コスト観測",
		"勝手にskip・縮退・最適化せず",
		"current taskのscopeを広げず",
		"反復コスト観測:",
	}
	for _, wire := range workerWires {
		if !strings.Contains(loopCostSection(t, "## 反復コスト観測", workerPrompt), wire) {
			t.Errorf("codex/glm-worker/prompts/WORKER.md lacks loop cost observation wiring: %q", wire)
		}
	}

	reviewerWires := []string{
		"## 反復コスト観測",
		"反復コスト観測:",
		"各review roundで増殖させない",
	}
	for _, wire := range reviewerWires {
		if !strings.Contains(loopCostSection(t, "## 反復コスト観測", reviewerPrompt), wire) {
			t.Errorf("codex/glm-worker/prompts/REVIEWER.md lacks loop cost observation wiring: %q", wire)
		}
	}

	executionWires := []string{
		"## machine executionの反復cost観測",
		"machine executionの反復を含む",
		"品質coverageを維持したまま実行回数・待ち時間・model/provider消費を減らせるか",
		"expensive real executionとcheap contract/mock verificationを分離できるか",
		"false success・flakiness・観測不能化を生まないか",
		"semanticに独立したfollow-up taskとして通常Plan lifecycleへ追加する",
		"改善効果が小さいものはtask化しない",
	}
	for _, wire := range executionWires {
		if !strings.Contains(loopCostSection(t, "## machine executionの反復cost観測", execution), wire) {
			t.Errorf("codex/instructions/glm-execution.md lacks machine execution loop cost wiring: %q", wire)
		}
	}

	observationSections := map[string]string{
		"codex/glm-worker/prompts/WORKER.md":   loopCostSection(t, "## 反復コスト観測", workerPrompt),
		"codex/glm-worker/prompts/REVIEWER.md": loopCostSection(t, "## 反復コスト観測", reviewerPrompt),
	}
	for path, section := range observationSections {
		if strings.Contains(section, "follow-up taskとして通常Plan lifecycleへ追加する") {
			t.Errorf("%s must not carry the parent task-ization judgment; it only reports observations", path)
		}
	}
	executionSection := loopCostSection(t, "## machine executionの反復cost観測", execution)
	if strings.Contains(executionSection, "反復コスト観測:") && strings.Contains(executionSection, "TESTSへ") {
		t.Error("codex/instructions/glm-execution.md must not duplicate the worker/reviewer reporting format contract")
	}

	for path, section := range map[string]string{
		"codex/glm-worker/prompts/WORKER.md":   loopCostSection(t, "## 反復コスト観測", workerPrompt),
		"codex/glm-worker/prompts/REVIEWER.md": loopCostSection(t, "## 反復コスト観測", reviewerPrompt),
		"codex/instructions/glm-execution.md":  executionSection,
	} {
		if strings.Contains(section, "install") {
			t.Errorf("%sの反復cost契約は特定scenario固有にしてはならない: install言及", path)
		}
	}

	smoke, err := os.ReadFile(filepath.Join(root, "tests", "install_smoke.sh"))
	if err != nil {
		t.Fatalf("read tests/install_smoke.sh: %v", err)
	}
	for _, wire := range []string{"expect_go_test_contract", "run_installer_xdg_override"} {
		if !strings.Contains(string(smoke), wire) {
			t.Errorf("tests/install_smoke.sh lacks go test invocation contract helper: %q", wire)
		}
	}

	eval := readContractFile("EVAL.md")
	for _, wire := range []string{
		"## machine executionの反復cost観測",
		"TestLoopCostObservationContractWiring",
		"tests/install-smoke-coverage.md",
	} {
		if !strings.Contains(eval, wire) {
			t.Errorf("EVAL.md lacks machine execution loop cost section wiring: %q", wire)
		}
	}
}

func loopCostSection(t *testing.T, header string, doc string) string {
	t.Helper()
	start := strings.Index(doc, header)
	if start < 0 {
		t.Fatalf("document lacks section header %q", header)
	}
	rest := doc[start:]
	if end := strings.Index(rest[len(header):], "\n## "); end >= 0 {
		rest = rest[:len(header)+end]
	}
	return rest
}
