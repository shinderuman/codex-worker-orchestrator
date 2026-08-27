package abeval

import (
	"os"
	"path/filepath"
	"testing"
)

type corpusPair struct {
	Spec                   Spec      `json:"spec"`
	Direct                 RunRecord `json:"direct"`
	Orchestrated           RunRecord `json:"orchestrated"`
	ExpectedCodexReduction string    `json:"expected_codex_reduction"`
}

type corpusFile struct {
	Version     int          `json:"version"`
	Description string       `json:"description"`
	Pairs       []corpusPair `json:"pairs"`
}

func corpusRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("glm-worker", "go.mod")
	for d := dir; d != string(filepath.Separator); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
			return d
		}
	}
	t.Fatalf("corpus root not found from %s", dir)
	return ""
}

func loadCorpus(t *testing.T) corpusFile {
	t.Helper()
	path := filepath.Join(corpusRepoRoot(t), "glm-worker", "scenarios", "ab-eval.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus corpusFile
	if err := decodeStrict(data, "ab-eval.json", &corpus); err != nil {
		t.Fatalf("ab-eval.json parse: %v", err)
	}
	return corpus
}

func TestABEvalCorpusDrivenThroughComparison(t *testing.T) {
	corpus := loadCorpus(t)
	covered := map[string]bool{}
	for _, pair := range corpus.Pairs {
		report := BuildReport(Compare(pair.Spec, pair.Direct, pair.Orchestrated))
		switch pair.ExpectedCodexReduction {
		case codexReductionActual:
			if report.CodexReduction.InputPercent == nil || report.CodexReduction.OutputPercent == nil {
				t.Fatalf("pair %sのcodex_reductionがactual usage基準ではありません: %+v", pair.Spec.ID, report.CodexReduction)
			}
			if report.CodexUsage.Direct == nil || report.CodexUsage.Orchestrated == nil {
				t.Fatalf("pair %sのactual codex_usageが欠けています: %+v", pair.Spec.ID, report.CodexUsage)
			}
		case codexReductionUnknown:
			if report.CodexReduction.InputPercent != nil || report.CodexReduction.OutputPercent != nil {
				t.Fatalf("pair %sのunknown経路で削減率percentが出ています: %+v", pair.Spec.ID, report.CodexReduction)
			}
			if report.CodexReduction.UnknownReason == "" {
				t.Fatalf("pair %sのunknown理由がありません: %+v", pair.Spec.ID, report.CodexReduction)
			}
		}
		if report.GLMUsage.Direct != nil {
			t.Fatalf("pair %sのdirect mode GLM使用はnullのべき: %+v", pair.Spec.ID, report.GLMUsage.Direct)
		}
		if report.Time.DirectMS == 0 || report.Time.OrchestratedMS == 0 {
			t.Fatalf("pair %sのtime導出がありません: %+v", pair.Spec.ID, report.Time)
		}
		covered[pair.ExpectedCodexReduction] = true
	}
	if !covered[codexReductionActual] || !covered[codexReductionUnknown] {
		t.Fatalf("corpusはactual/unknown両経路をcoverする必要があります: %v", covered)
	}
}
