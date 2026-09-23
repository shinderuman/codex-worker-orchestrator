package abeval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIncompleteCodexUsageCannotBecomeActualReduction(t *testing.T) {
	tests := []struct {
		name  string
		usage map[string]any
	}{
		{name: "source only", usage: map[string]any{"source": CodexUsageSourceAppExport}},
		{name: "both null", usage: map[string]any{"source": CodexUsageSourceAppExport, "input_tokens": nil, "output_tokens": nil}},
		{name: "input missing", usage: map[string]any{"source": CodexUsageSourceAppExport, "output_tokens": 42}},
		{name: "input null", usage: map[string]any{"source": CodexUsageSourceAppExport, "input_tokens": nil, "output_tokens": 42}},
		{name: "output missing", usage: map[string]any{"source": CodexUsageSourceAppExport, "input_tokens": 42}},
		{name: "output null", usage: map[string]any{"source": CodexUsageSourceAppExport, "input_tokens": 42, "output_tokens": nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec()
			data := marshalRecordWithCodexUsage(t, validOrchestratedRecord(spec), test.usage)
			path := filepath.Join(t.TempDir(), "orchestrated.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			record, err := LoadRecord(path)
			if err != nil {
				return
			}
			direct := validDirectRecord(spec)
			if err := ValidatePair(spec, direct, record); err != nil {
				return
			}
			report := BuildReport(Compare(spec, direct, record))
			if report.CodexReduction.Status == codexReductionActual {
				t.Fatalf("未観測token fieldがactual削減率になりました: %+v", report.CodexReduction)
			}
		})
	}
}

func TestExplicitZeroCodexUsageRemainsObserved(t *testing.T) {
	spec := validSpec()
	record := validOrchestratedRecord(spec)
	record.CodexUsage.InputTokens = 0
	record.CodexUsage.OutputTokens = 0
	path := writeRecordJSON(t, "orchestrated.json", record)

	loaded, err := LoadRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	direct := validDirectRecord(spec)
	if err := ValidatePair(spec, direct, loaded); err != nil {
		t.Fatal(err)
	}
	report := BuildReport(Compare(spec, direct, loaded))
	if report.CodexReduction.Status != codexReductionActual {
		t.Fatalf("明示0のobserved usageがunknownになりました: %+v", report.CodexReduction)
	}
	if report.CodexReduction.InputPercent == nil || *report.CodexReduction.InputPercent != 100 {
		t.Fatalf("input削減率 = %v, want 100", report.CodexReduction.InputPercent)
	}
	if report.CodexReduction.OutputPercent == nil || *report.CodexReduction.OutputPercent != 100 {
		t.Fatalf("output削減率 = %v, want 100", report.CodexReduction.OutputPercent)
	}
}

func TestCodexUsageJSONRejectsNestedUnknownField(t *testing.T) {
	data := marshalOrFatal(t, map[string]any{
		"source":        CodexUsageSourceAppExport,
		"input_tokens":  1,
		"output_tokens": 1,
		"extra":         1,
	})
	var usage CodexUsage
	if err := json.Unmarshal(data, &usage); err == nil {
		t.Fatal("codex_usageの未知fieldが受理されました")
	}
}

func marshalRecordWithCodexUsage(t *testing.T, record RunRecord, usage map[string]any) []byte {
	t.Helper()
	data := marshalOrFatal(t, record)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["codex_usage"] = usage
	return marshalOrFatal(t, raw)
}
