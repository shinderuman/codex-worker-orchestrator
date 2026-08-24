package abeval

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func compareFixture() Comparison {
	spec := validSpec()
	return Compare(spec, validDirectRecord(spec), validOrchestratedRecord(spec))
}

func TestCodexReductionComputesFromActualUsage(t *testing.T) {
	reduction := compareFixture().CodexReduction
	if reduction.Status != codexReductionActual {
		t.Fatalf("status = %q want %q", reduction.Status, codexReductionActual)
	}
	if got, want := reduction.InputPercent, 60.0; got != want {
		t.Fatalf("input削減率 = %v want %v", got, want)
	}
	if got, want := reduction.OutputPercent, 53.333333333333336; got != want {
		t.Fatalf("output削減率 = %v want %v", got, want)
	}
}

func TestCodexReductionStaysUnknownWithoutActualUsage(t *testing.T) {
	tests := []struct {
		name         string
		direct       CodexUsage
		orchestrated CodexUsage
		wantReason   string
	}{
		{
			name:         "both unknown",
			direct:       CodexUsage{},
			orchestrated: CodexUsage{},
			wantReason:   "direct,orchestrated",
		},
		{
			name:         "orchestrated unknown",
			direct:       CodexUsage{Source: CodexUsageSourceAppExport, InputTokens: 100},
			orchestrated: CodexUsage{},
			wantReason:   "orchestrated",
		},
		{
			name:         "direct unknown",
			direct:       CodexUsage{},
			orchestrated: CodexUsage{Source: CodexUsageSourceAppExport, InputTokens: 100},
			wantReason:   "direct",
		},
		{
			name:         "actual but zero tokens",
			direct:       CodexUsage{Source: CodexUsageSourceAppExport},
			orchestrated: CodexUsage{Source: CodexUsageSourceAppExport},
			wantReason:   "零のため",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			reduction := codexReduction(test.direct, test.orchestrated)
			if reduction.Status != codexReductionUnknown {
				t.Fatalf("status = %q want unknown", reduction.Status)
			}
			if !strings.Contains(reduction.UnknownReason, test.wantReason) {
				t.Fatalf("reason = %q want substring %q", reduction.UnknownReason, test.wantReason)
			}
		})
	}
}

func TestCodexReductionReportsNegativeWhenOrchestratedUsesMore(t *testing.T) {
	reduction := codexReduction(
		CodexUsage{Source: CodexUsageSourceAppExport, InputTokens: 100},
		CodexUsage{Source: CodexUsageSourceAppExport, InputTokens: 150},
	)
	if reduction.InputPercent != -50.0 {
		t.Fatalf("input削減率 = %v want -50", reduction.InputPercent)
	}
}

func TestBuildReportCarriesMachineContractFields(t *testing.T) {
	report := BuildReport(compareFixture())

	if report.SpecID != "fixed-eval-example" {
		t.Fatalf("spec_id = %q", report.SpecID)
	}
	if fmt.Sprint(report.Modes) != fmt.Sprint([]string{"direct", "orchestrated"}) {
		t.Fatalf("modes = %v", report.Modes)
	}
	if report.MeasurementBoundary != CanonicalMeasurementBoundary {
		t.Fatalf("measurement_boundary = %q", report.MeasurementBoundary)
	}
	if !report.Isolation.IndependentSession || !report.Isolation.IndependentWorktree {
		t.Fatalf("isolation = %+v", report.Isolation)
	}
	if report.CodexReduction.Status != codexReductionActual {
		t.Fatalf("codex_reduction.status = %q", report.CodexReduction.Status)
	}
	if report.CodexReduction.InputPercent == nil || *report.CodexReduction.InputPercent != 60.0 {
		t.Fatalf("codex_reduction.input_percent = %+v", report.CodexReduction.InputPercent)
	}
	if report.CodexReduction.OutputPercent == nil || *report.CodexReduction.OutputPercent != 53.333333333333336 {
		t.Fatalf("codex_reduction.output_percent = %+v", report.CodexReduction.OutputPercent)
	}
	if report.CodexReduction.DirectSource != CodexUsageSourceAppExport || report.CodexReduction.OrchestratedSource != CodexUsageSourceAppExport {
		t.Fatalf("codex_reduction sources = %q/%q", report.CodexReduction.DirectSource, report.CodexReduction.OrchestratedSource)
	}
	if report.Time.DirectMS != 92*time.Minute.Milliseconds() || report.Time.OrchestratedMS != 58*time.Minute.Milliseconds() ||
		report.Time.DeltaMS != -34*time.Minute.Milliseconds() {
		t.Fatalf("time = %+v", report.Time)
	}
	if report.QualityDelta.Direct.TestsRun != 246 || report.QualityDelta.Orchestrated.TestsRun != 246 {
		t.Fatalf("quality_delta = %+v", report.QualityDelta)
	}
}

func TestBuildReportKeepsProxyMetricsSeparateFromCodexUsage(t *testing.T) {
	report := BuildReport(compareFixture())

	if report.CodexUsage.Direct == nil || report.CodexUsage.Direct.InputTokens != 1200000 || report.CodexUsage.Direct.OutputTokens != 90000 {
		t.Fatalf("codex_usage.direct = %+v", report.CodexUsage.Direct)
	}
	if report.CodexUsage.Orchestrated == nil || report.CodexUsage.Orchestrated.InputTokens != 480000 || report.CodexUsage.Orchestrated.OutputTokens != 42000 {
		t.Fatalf("codex_usage.orchestrated = %+v", report.CodexUsage.Orchestrated)
	}
	if report.ProxyMetrics.Direct != nil {
		t.Fatalf("direct modeのproxy観測はない: %+v", report.ProxyMetrics.Direct)
	}
	if report.ProxyMetrics.Orchestrated == nil || report.ProxyMetrics.Orchestrated.SolPacketBytes != 812 {
		t.Fatalf("proxy_metrics.orchestrated = %+v", report.ProxyMetrics.Orchestrated)
	}
}

func TestBuildReportDirectGLMUsageIsNullAndOrchestratedKeepsActuals(t *testing.T) {
	report := BuildReport(compareFixture())

	if report.GLMUsage.Direct != nil {
		t.Fatalf("direct modeはglm-worker委譲なしのためnull: %+v", report.GLMUsage.Direct)
	}
	orchestrated := report.GLMUsage.Orchestrated
	if orchestrated == nil || orchestrated.Source != GLMUsageSourceTaskStats || orchestrated.InputTokens != 850000 || orchestrated.ModelCalls != 5 {
		t.Fatalf("glm_usage.orchestrated = %+v", orchestrated)
	}
}

func TestBuildReportShowsUnknownReductionWithoutFabricatedPercent(t *testing.T) {
	spec := validSpec()
	direct := validDirectRecord(spec)
	direct.CodexUsage = CodexUsage{}
	orchestrated := validOrchestratedRecord(spec)
	report := BuildReport(Compare(spec, direct, orchestrated))

	if report.CodexReduction.Status != codexReductionUnknown || report.CodexReduction.UnknownReason == "" {
		t.Fatalf("unknown削減率の根拠が出ていません: %+v", report.CodexReduction)
	}
	if report.CodexReduction.InputPercent != nil || report.CodexReduction.OutputPercent != nil {
		t.Fatalf("actual usageがないのに削減率percentが出ています: %+v", report.CodexReduction)
	}
	if report.CodexUsage.Direct != nil {
		t.Fatalf("direct usage unknownはnull: %+v", report.CodexUsage.Direct)
	}
}

// TestBuildReportJSONKeysAreStableは--eval-ab成功JSONのtop-level key集合を固定する。
func TestBuildReportJSONKeysAreStable(t *testing.T) {
	data, err := json.Marshal(BuildReport(compareFixture()))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"spec_id", "modes", "metadata", "measurement_boundary", "isolation",
		"codex_reduction", "quality_delta", "time", "codex_usage", "glm_usage", "proxy_metrics",
	}
	if len(object) != len(want) {
		t.Fatalf("key数 = %d want %d: %v", len(object), len(want), object)
	}
	for _, key := range want {
		if _, ok := object[key]; !ok {
			t.Fatalf("key %qがありません: %s", key, data)
		}
	}
	var metadata struct {
		UserRequestSHA256 string `json:"user_request_sha256"`
	}
	if err := json.Unmarshal(object["metadata"], &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.UserRequestSHA256) != 64 {
		t.Fatalf("user_request_sha256は全文hash: %q", metadata.UserRequestSHA256)
	}
}
