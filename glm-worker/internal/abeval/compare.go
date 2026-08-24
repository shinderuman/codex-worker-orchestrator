package abeval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	codexReductionActual  = "actual"
	codexReductionUnknown = "unknown"
)

// Comparisonは検証済みrun記録1組の集計結果。最重要出力のCodexReductionと品質比較、
// および時間とGLM usageを別枠で持つ。GLM tokenとCodex tokenを合算した総合値fieldは
// 持たない。
type Comparison struct {
	Spec                 Spec
	Direct               RunRecord
	Orchestrated         RunRecord
	CodexReduction       CodexReduction
	DirectDuration       time.Duration
	OrchestratedDuration time.Duration
}

// CodexReductionはactual Codex使用量に基づくDirect比の削減率。両modeのactual usageが
// 揃わない場合はStatus=unknownとし、proxy指標や推定値で代替しない。
type CodexReduction struct {
	Status        string
	UnknownReason string
	InputPercent  float64
	OutputPercent float64
}

func Compare(spec Spec, direct, orchestrated RunRecord) Comparison {
	return Comparison{
		Spec:                 spec,
		Direct:               direct,
		Orchestrated:         orchestrated,
		CodexReduction:       codexReduction(direct.CodexUsage, orchestrated.CodexUsage),
		DirectDuration:       direct.Boundary.CompletedAt.Sub(direct.Boundary.StartedAt),
		OrchestratedDuration: orchestrated.Boundary.CompletedAt.Sub(orchestrated.Boundary.StartedAt),
	}
}

func codexReduction(direct, orchestrated CodexUsage) CodexReduction {
	var missing []string
	if !direct.Known() {
		missing = append(missing, "direct")
	}
	if !orchestrated.Known() {
		missing = append(missing, "orchestrated")
	}
	if len(missing) > 0 {
		return CodexReduction{
			Status:        codexReductionUnknown,
			UnknownReason: fmt.Sprintf("actual Codex usageが公式/runtime telemetryから取得できていません: %s", strings.Join(missing, ",")),
		}
	}
	if direct.InputTokens <= 0 && direct.OutputTokens <= 0 {
		return CodexReduction{
			Status:        codexReductionUnknown,
			UnknownReason: "direct actual usageのtoken値が零のため削減率を定義できません",
		}
	}
	result := CodexReduction{Status: codexReductionActual}
	if direct.InputTokens > 0 {
		result.InputPercent = reductionPercent(direct.InputTokens, orchestrated.InputTokens)
	}
	if direct.OutputTokens > 0 {
		result.OutputPercent = reductionPercent(direct.OutputTokens, orchestrated.OutputTokens)
	}
	return result
}

func reductionPercent(direct, orchestrated int64) float64 {
	return float64(direct-orchestrated) / float64(direct) * 100
}

// Reportは--eval-ab成功時のmachine contract。actual usageとproxy指標・unknownを
// JSON型で区別して載せる。GLM tokenとCodex tokenを合算した総合値fieldは持たない。
type Report struct {
	SpecID              string                `json:"spec_id"`
	Modes               []string              `json:"modes"`
	Metadata            ReportMetadata        `json:"metadata"`
	MeasurementBoundary string                `json:"measurement_boundary"`
	Isolation           IsolationRequirements `json:"isolation"`
	CodexReduction      ReportReduction       `json:"codex_reduction"`
	QualityDelta        ReportQualityDelta    `json:"quality_delta"`
	Time                ReportTime            `json:"time"`
	CodexUsage          ReportCodexUsagePair  `json:"codex_usage"`
	GLMUsage            ReportGLMUsagePair    `json:"glm_usage"`
	ProxyMetrics        ReportProxyPair       `json:"proxy_metrics"`
}

// ReportMetadataは両mode共通の比較条件。UserRequestSHA256は要求本文の正準hash全文。
type ReportMetadata struct {
	RepoSnapshotCommit string `json:"repo_snapshot_commit"`
	InitialWorktree    string `json:"initial_worktree"`
	CodexModel         string `json:"codex_model"`
	CodexReasoning     string `json:"codex_reasoning"`
	UserRequestSHA256  string `json:"user_request_sha256"`
}

// ReportReductionはactual Codex使用量に基づくDirect比の削減率。Statusはactualか
// unknownで、unknownのときUnknownReasonだけが根拠を運びpercentは出さない。
type ReportReduction struct {
	Status             string   `json:"status"`
	UnknownReason      string   `json:"unknown_reason,omitempty"`
	InputPercent       *float64 `json:"input_percent,omitempty"`
	OutputPercent      *float64 `json:"output_percent,omitempty"`
	DirectSource       string   `json:"direct_source,omitempty"`
	OrchestratedSource string   `json:"orchestrated_source,omitempty"`
}

type ReportQualityDelta struct {
	Direct       Quality `json:"direct"`
	Orchestrated Quality `json:"orchestrated"`
}

type ReportTime struct {
	DirectMS       int64 `json:"direct_ms"`
	OrchestratedMS int64 `json:"orchestrated_ms"`
	DeltaMS        int64 `json:"delta_ms"`
}

// ReportCodexUsagePairは両modeのactual Codex使用量。unknownのときnull。
type ReportCodexUsagePair struct {
	Direct       *CodexUsage `json:"direct"`
	Orchestrated *CodexUsage `json:"orchestrated"`
}

// ReportGLMUsagePairはglm-worker側実測使用量。direct modeはglm-worker委譲がないため
// null、orchestratedはrecord解決済みの実測値。
type ReportGLMUsagePair struct {
	Direct       *GLMUsage `json:"direct"`
	Orchestrated *GLMUsage `json:"orchestrated"`
}

// ReportProxyPairはactual usageではない代理指標。観測がないmodeはnull。
type ReportProxyPair struct {
	Direct       *ProxyMetrics `json:"direct"`
	Orchestrated *ProxyMetrics `json:"orchestrated"`
}

// BuildReportは比較結果をmachine contractのReportへ組み立てる。
func BuildReport(c Comparison) Report {
	reduction := ReportReduction{Status: c.CodexReduction.Status}
	if c.CodexReduction.Status == codexReductionUnknown {
		reduction.UnknownReason = c.CodexReduction.UnknownReason
	} else {
		if c.Direct.CodexUsage.InputTokens > 0 {
			reduction.InputPercent = &c.CodexReduction.InputPercent
		}
		if c.Direct.CodexUsage.OutputTokens > 0 {
			reduction.OutputPercent = &c.CodexReduction.OutputPercent
		}
		reduction.DirectSource = c.Direct.CodexUsage.Source
		reduction.OrchestratedSource = c.Orchestrated.CodexUsage.Source
	}
	return Report{
		SpecID: c.Spec.ID,
		Modes:  []string{string(ModeDirect), string(ModeOrchestrated)},
		Metadata: ReportMetadata{
			RepoSnapshotCommit: c.Spec.RepoSnapshotCommit,
			InitialWorktree:    c.Spec.InitialWorktree,
			CodexModel:         c.Spec.CodexModel,
			CodexReasoning:     c.Spec.CodexReasoningEffort,
			UserRequestSHA256:  requestSHA256(c.Spec.UserRequest),
		},
		MeasurementBoundary: c.Spec.MeasurementBoundary,
		Isolation:           c.Spec.Isolation,
		CodexReduction:      reduction,
		QualityDelta: ReportQualityDelta{
			Direct:       c.Direct.Quality,
			Orchestrated: c.Orchestrated.Quality,
		},
		Time: ReportTime{
			DirectMS:       c.DirectDuration.Milliseconds(),
			OrchestratedMS: c.OrchestratedDuration.Milliseconds(),
			DeltaMS:        (c.OrchestratedDuration - c.DirectDuration).Milliseconds(),
		},
		CodexUsage: ReportCodexUsagePair{
			Direct:       codexUsagePtr(c.Direct.CodexUsage),
			Orchestrated: codexUsagePtr(c.Orchestrated.CodexUsage),
		},
		GLMUsage: ReportGLMUsagePair{
			Direct:       nil,
			Orchestrated: &c.Orchestrated.GLMUsage,
		},
		ProxyMetrics: ReportProxyPair{
			Direct:       proxyPtr(c.Direct.Proxy),
			Orchestrated: proxyPtr(c.Orchestrated.Proxy),
		},
	}
}

func codexUsagePtr(usage CodexUsage) *CodexUsage {
	if !usage.Known() {
		return nil
	}
	return &usage
}

func proxyPtr(proxy ProxyMetrics) *ProxyMetrics {
	if proxy == (ProxyMetrics{}) {
		return nil
	}
	return &proxy
}

func requestSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
