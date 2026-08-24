package packet

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// このfile群はTask 008 machine protocol測定のtest-only harnessであり、production
// protocolへimportされない。旧形式の参照実装は測定対象の正確さのためだけに存在する。

// legacyFieldは旧KEY行protocolの表示行1件。key/valueは202bc92^時点の
// packet.Result.displayFieldsと同じ対応。
type legacyField struct {
	key   string
	value string
}

// legacyDisplayFieldsはTask 006直前(202bc92^ = 677e469。fdb9c41..677e469間で
// result.goの表示codeは不変)のKEY行protocol実装の測定用移植。status別field順・空配列の
// `none` placeholder・空text fieldの行出力まで当時の挙動を維持する。
func legacyDisplayFields(r Result) []legacyField {
	text := func(key string, value string) legacyField {
		return legacyField{key: key, value: value}
	}
	switch r.Status {
	case StatusNeedsSolDecision:
		fields := []legacyField{
			text("STATUS", string(r.Status)),
			text("RISK", string(r.Risk)),
			text("DECISION", r.Decision),
			text("EVIDENCE", r.Evidence),
			text("OPTIONS", r.Options),
			text("RECOMMENDATION", r.Recommendation),
			text("TEST_OBLIGATIONS", r.TestObligations),
		}
		fields = append(fields, legacyTargetsField(r))
		return append(fields, legacyArtifactsField(r))
	case StatusImplemented:
		fields := []legacyField{
			text("STATUS", string(r.Status)),
			text("RISK", string(r.Risk)),
			text("SUMMARY", r.Summary),
			text("REQUIREMENT_COVERAGE", r.RequirementCoverage),
			text("TESTS", r.Tests),
			text("UNVERIFIED", r.Unverified),
		}
		if len(r.Targets) > 0 {
			fields = append(fields, legacyTargetsField(r))
		}
		return append(fields, legacyArtifactsField(r))
	default:
		fields := []legacyField{
			text("STATUS", string(r.Status)),
			text("RISK", string(r.Risk)),
			text("SUMMARY", r.Summary),
			text("REQUIREMENT_COVERAGE", r.RequirementCoverage),
			text("INVARIANTS", r.Invariants),
			text("TEST_EVIDENCE", r.TestEvidence),
			text("ISSUES", r.Issues),
			text("RESIDUAL_RISK", r.ResidualRisk),
			legacyTargetsField(r),
			legacyArtifactsField(r),
		}
		if r.SolQuestion != "" {
			fields = append(fields, text("SOL_QUESTION", r.SolQuestion))
		}
		return fields
	}
}

func legacyTargetsField(r Result) legacyField {
	return legacyField{key: "TARGETS", value: legacyJoinList(r.Targets)}
}

func legacyArtifactsField(r Result) legacyField {
	return legacyField{key: "ARTIFACTS", value: legacyJoinList(r.Artifacts)}
}

func legacyJoinList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ";")
}

// legacyDisplayLinesは旧protocolの表示行を返す。
func legacyDisplayLines(r Result) []string {
	fields := legacyDisplayFields(r)
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		lines = append(lines, field.key+": "+field.value)
	}
	return lines
}

// legacyDisplayはTask 006前のwrapper最終stdout本文。emitResultはFprintlnで
// 末尾に改行を1つ足していた。
func legacyDisplay(r Result) string {
	return strings.Join(legacyDisplayLines(r), "\n")
}

// legacyFromDisplayLinesは旧text PACKET形式のdecoder(v2 resume checkpoint読込で
// 使われた202bc92^のFromDisplayLines)の測定用移植。information loss測定の対として
// 使い、productionの受理経路ではない。
func legacyFromDisplayLines(lines []string) (Result, error) {
	fields := make(map[string]string, len(lines))
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Result{}, fmt.Errorf("旧packet行をKEY: value形式へ解析できません: %q", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return Result{}, fmt.Errorf("旧packet行のKEYが空です: %q", line)
		}
		if _, exists := fields[key]; exists {
			return Result{}, fmt.Errorf("旧packet field %sが重複しています", key)
		}
		fields[key] = strings.TrimSpace(value)
	}
	if fields["STATUS"] == "" {
		return Result{}, fmt.Errorf("旧packetにSTATUSがありません")
	}
	result := Result{
		Status:              Status(fields["STATUS"]),
		Risk:                Risk(fields["RISK"]),
		Summary:             fields["SUMMARY"],
		RequirementCoverage: fields["REQUIREMENT_COVERAGE"],
		Tests:               fields["TESTS"],
		Unverified:          fields["UNVERIFIED"],
		Decision:            fields["DECISION"],
		Evidence:            fields["EVIDENCE"],
		Options:             fields["OPTIONS"],
		Recommendation:      fields["RECOMMENDATION"],
		TestObligations:     fields["TEST_OBLIGATIONS"],
		Invariants:          fields["INVARIANTS"],
		TestEvidence:        fields["TEST_EVIDENCE"],
		Issues:              fields["ISSUES"],
		ResidualRisk:        fields["RESIDUAL_RISK"],
		SolQuestion:         fields["SOL_QUESTION"],
	}
	if targets := legacySplitDisplayList(fields["TARGETS"]); len(targets) > 0 {
		result.Targets = targets
	}
	if artifacts := legacySplitDisplayList(fields["ARTIFACTS"]); len(artifacts) > 0 {
		result.Artifacts = artifacts
	}
	return result, nil
}

// legacySplitDisplayListは当時の実装どおり`none`・空を要素なし扱いへ潰す。
// この潰しがreviewer TARGETS予約値`none`の意味喪失になることを測定で固定する。
func legacySplitDisplayList(value string) []string {
	if value == "" || value == noneTargetsSentinel {
		return nil
	}
	parts := strings.Split(value, ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// TestLegacyRendererMatchesPreTask006Productionは移植rendererのbyte一致を
// 202bc92^時点のproduction test(packet/result_test.go TestResultDisplayLines)の
// 期待値原文で固定する。測定の正確さがこの一致に依存するため、vectorは当時の
// production helper(implementedResult相当)と同一入力で与える。
func TestLegacyRendererMatchesPreTask006Production(t *testing.T) {
	implemented := Result{
		Status:              StatusImplemented,
		Risk:                RiskLow,
		Summary:             "s",
		RequirementCoverage: "c",
		Tests:               "t",
		Unverified:          "none",
	}
	want := []string{
		"STATUS: IMPLEMENTED",
		"RISK: LOW",
		"SUMMARY: s",
		"REQUIREMENT_COVERAGE: c",
		"TESTS: t",
		"UNVERIFIED: none",
		"ARTIFACTS: none",
	}
	if got := legacyDisplayLines(implemented); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("display = %v", got)
	}

	implemented.Targets = []string{"a.go", "b.go"}
	if !strings.Contains(legacyDisplay(implemented), "TARGETS: a.go;b.go") {
		t.Fatalf("targets not joined: %s", legacyDisplay(implemented))
	}

	decision := Result{
		Status:          StatusNeedsSolDecision,
		Risk:            RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "t",
	}
	decisionDisplay := legacyDisplay(decision)
	for _, want := range []string{"DECISION: d", "TARGETS: none", "ARTIFACTS: none"} {
		if !strings.Contains(decisionDisplay, want) {
			t.Fatalf("decision display missing %q: %s", want, decisionDisplay)
		}
	}

	review := Result{
		Status:       StatusNeedsSolReview,
		Risk:         RiskHigh,
		Summary:      "s",
		TestEvidence: "e",
		Targets:      []string{"a.go"},
		SolQuestion:  "q",
		Invariants:   "i",
		Issues:       "n",
		ResidualRisk: "n",
	}
	reviewDisplay := legacyDisplayLines(review)
	if reviewDisplay[len(reviewDisplay)-1] != "SOL_QUESTION: q" {
		t.Fatalf("SOL_QUESTION must render last: %v", reviewDisplay)
	}
}

// TestLegacyRoundTripMatchesPreTask006Productionは当時の
// TestFromDisplayLinesRoundTripと同一入力・同一比較でdecoder移植を固定する。
func TestLegacyRoundTripMatchesPreTask006Production(t *testing.T) {
	result := Result{
		Status:              StatusNeedsSolReview,
		Risk:                RiskHigh,
		Summary:             "s",
		RequirementCoverage: "c",
		Invariants:          "i",
		TestEvidence:        "e",
		Issues:              "none",
		ResidualRisk:        "r",
		Targets:             []string{"a.go:f", "b.go"},
		Artifacts:           []string{"/tmp/x"},
		SolQuestion:         "q",
	}
	parsed, err := legacyFromDisplayLines(legacyDisplayLines(result))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(want) {
		t.Fatalf("round trip mismatch:\n%s\n%s", encoded, want)
	}
}
