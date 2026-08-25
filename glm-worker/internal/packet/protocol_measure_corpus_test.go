package packet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tokenProxyRunBased(s string) int {
	count := 0
	runClass := byte(0)
	flush := func() {
		if runClass != 0 {
			count++
			runClass = 0
		}
	}
	for _, r := range s {
		switch {
		case r > '~':
			flush()
			count++
		case isProxyWordRune(r):
			if runClass != 'w' {
				flush()
				runClass = 'w'
			}
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			if runClass != 'p' {
				flush()
				runClass = 'p'
			}
		}
	}
	flush()
	return count
}

func tokenProxyCharPunct(s string) int {
	count := 0
	inWord := false
	for _, r := range s {
		switch {
		case isProxyWordRune(r):
			if !inWord {
				count++
				inWord = true
			}
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inWord = false
		default:
			count++
			inWord = false
		}
	}
	return count
}

func isProxyWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func semanticValueBytes(r Result) int {
	total := len(r.Summary) + len(r.RequirementCoverage) + len(r.Tests) + len(r.Unverified) +
		len(r.Decision) + len(r.Evidence) + len(r.Options) + len(r.Recommendation) +
		len(r.TestObligations) + len(r.Invariants) + len(r.TestEvidence) + len(r.Issues) +
		len(r.ResidualRisk) + len(r.SolQuestion) + len(r.Status) + len(r.Risk)
	for _, element := range r.Targets {
		total += len(element)
	}
	for _, element := range r.Artifacts {
		total += len(element)
	}
	return total
}

type renderMeasurement struct {
	Format          string
	StdoutBytes     int
	TokensRun       int
	TokensCharPunct int
	SemanticBytes   int
	StructuredBytes int

	NoiseFields int

	DuplicateValues int
}

func measureRendered(format string, rendered string, value Result) renderMeasurement {
	projected := contractSemantics(value)
	m := renderMeasurement{
		Format:          format,
		StdoutBytes:     len(rendered) + 1,
		TokensRun:       tokenProxyRunBased(rendered),
		TokensCharPunct: tokenProxyCharPunct(rendered),
		SemanticBytes:   semanticValueBytes(projected),
	}
	m.StructuredBytes = m.StdoutBytes - m.SemanticBytes
	values := map[string]int{}
	addValues(values, projected)
	for _, count := range values {
		if count > 1 {
			m.DuplicateValues += count - 1
		}
	}
	return m
}

func addValues(values map[string]int, value Result) {
	bump := func(s string) {
		if s != "" && s != noneTargetsSentinel && s != ReportOnlyTargets {
			values[s]++
		}
	}
	bump(string(value.Status))
	bump(string(value.Risk))
	for _, s := range []string{
		value.Summary, value.RequirementCoverage, value.Tests, value.Unverified,
		value.Decision, value.Evidence, value.Options, value.Recommendation,
		value.TestObligations, value.Invariants, value.TestEvidence, value.Issues,
		value.ResidualRisk, value.SolQuestion,
	} {
		bump(s)
	}
	for _, element := range value.Targets {
		bump(element)
	}
	for _, element := range value.Artifacts {
		bump(element)
	}
}

func measureLegacyNoise(value Result) int {
	noise := 0
	for _, field := range legacyDisplayFields(value) {
		if field.value == "" {
			noise++
		}
	}
	if len(value.Targets) == 0 && value.Status != StatusImplemented {
		noise++
	}
	if len(value.Artifacts) == 0 {
		noise++
	}
	return noise
}

func measureLegacy(value Result) renderMeasurement {
	m := measureRendered("legacy-keyline", legacyDisplay(value), value)
	m.NoiseFields = measureLegacyNoise(value)
	return m
}

func measureMachine(value Result) renderMeasurement {
	data, err := value.MachineJSON()
	if err != nil {
		panic(err)
	}
	return measureRendered("machine-json", string(data), value)
}

func sameResultSemantics(a Result, b Result) bool {
	equalList := func(x []string, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return a.Status == b.Status &&
		a.Risk == b.Risk &&
		a.Summary == b.Summary &&
		a.RequirementCoverage == b.RequirementCoverage &&
		a.Tests == b.Tests &&
		a.Unverified == b.Unverified &&
		a.Decision == b.Decision &&
		a.Evidence == b.Evidence &&
		a.Options == b.Options &&
		a.Recommendation == b.Recommendation &&
		a.TestObligations == b.TestObligations &&
		a.Invariants == b.Invariants &&
		a.TestEvidence == b.TestEvidence &&
		a.Issues == b.Issues &&
		a.ResidualRisk == b.ResidualRisk &&
		a.SolQuestion == b.SolQuestion &&
		equalList(a.Targets, b.Targets) &&
		equalList(a.Artifacts, b.Artifacts)
}

func contractSemantics(value Result) Result {
	projected := Result{
		Status:    value.Status,
		Risk:      value.Risk,
		Targets:   value.Targets,
		Artifacts: value.Artifacts,
	}
	textFields := map[string]*string{
		"summary":              &projected.Summary,
		"requirement_coverage": &projected.RequirementCoverage,
		"tests":                &projected.Tests,
		"unverified":           &projected.Unverified,
		"decision":             &projected.Decision,
		"evidence":             &projected.Evidence,
		"options":              &projected.Options,
		"recommendation":       &projected.Recommendation,
		"test_obligations":     &projected.TestObligations,
		"invariants":           &projected.Invariants,
		"test_evidence":        &projected.TestEvidence,
		"issues":               &projected.Issues,
		"residual_risk":        &projected.ResidualRisk,
		"sol_question":         &projected.SolQuestion,
	}
	for _, field := range value.contractFields() {
		*textFields[field.machine] = field.value(value)
	}
	return projected
}

func hasNonContractNoise(value Result) bool {
	return !sameResultSemantics(value, contractSemantics(value))
}

type measuredPayload struct {
	Name   string
	Source string
	Value  Result
}

func syntheticMeasuredPayloads() []measuredPayload {
	maxField := strings.Repeat("x", 1000) + strings.Repeat("語", 100)
	implementedNone := implementedResult()
	implementedNone.Targets = []string{noneTargetsSentinel}
	implementedArtifacts := implementedResult()
	implementedArtifacts.Artifacts = []string{"/artifacts/task/report.md"}
	decision := Result{
		Status:          StatusNeedsSolDecision,
		Risk:            RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "t",
		Targets:         []string{noneTargetsSentinel},
	}
	reviewNoneTargets := passResult()
	reviewNoneTargets.Targets = []string{noneTargetsSentinel}
	reportOnly := passResult()
	reportOnly.Status = StatusFixRequired
	reportOnly.Risk = RiskHigh
	reportOnly.Targets = []string{ReportOnlyTargets}
	semicolonTarget := passResult()
	semicolonTarget.Targets = []string{"glm-worker/internal/packet/a.go:1;b.go:2"}
	maximal := implementedResult()
	maximal.Summary = maxField
	maximal.Unverified = "none"
	return []measuredPayload{
		{"synthetic worker IMPLEMENTED minimal", "", implementedResult()},
		{"synthetic worker IMPLEMENTED targets none sentinel", "", implementedNone},
		{"synthetic worker IMPLEMENTED with artifacts", "", implementedArtifacts},
		{"synthetic worker IMPLEMENTED max field", "", maximal},
		{"synthetic worker NEEDS_SOL_DECISION targets none", "", decision},
		{"synthetic reviewer PASS minimal", "", passResult()},
		{"synthetic reviewer PASS targets none sentinel", "", reviewNoneTargets},
		{"synthetic reviewer FIX_REQUIRED report-only PACKET", "", reportOnly},
		{"synthetic reviewer PASS semicolon element", "", semicolonTarget},
	}
}

func realMeasuredPayloads(t *testing.T) []measuredPayload {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "protocol-measure-results.json"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var corpus struct {
		Description string `json:"description"`
		Entries     []struct {
			Source struct {
				Repo  string `json:"repo"`
				Task  string `json:"task"`
				Call  string `json:"call"`
				At    string `json:"at"`
				Era   string `json:"era"`
				Phase string `json:"phase"`
			} `json:"source"`
			Result json.RawMessage `json:"result"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("err = %v", err)
	}
	payloads := make([]measuredPayload, 0, len(corpus.Entries))
	for _, entry := range corpus.Entries {
		value, err := ParseStructured(entry.Result)
		if err != nil {
			t.Fatalf("%s: err = %v", entry.Source.Call, err)
		}
		name := "real " + entry.Source.Era + " " + string(value.Status) + " " + entry.Source.Repo + " " + entry.Source.Call
		payloads = append(payloads, measuredPayload{Name: name, Source: entry.Source.Task + " " + entry.Source.At, Value: value})
	}
	return payloads
}

func allMeasuredPayloads(t *testing.T) []measuredPayload {
	t.Helper()
	return append(syntheticMeasuredPayloads(), realMeasuredPayloads(t)...)
}

func legacyRoundTripLoss(value Result) string {
	joined := legacyJoinList(value.Targets)
	parsed := legacySplitDisplayList(joined)
	if len(parsed) != len(value.Targets) {
		return "targets要素のセミコロン分割"
	}
	for i := range parsed {
		if parsed[i] != strings.TrimSpace(value.Targets[i]) {
			return "targets要素のセミコロン分割"
		}
	}
	return ""
}

func TestProtocolMeasurementSemanticRetention(t *testing.T) {
	for _, payload := range allMeasuredPayloads(t) {
		t.Run(payload.Name, func(t *testing.T) {
			want := contractSemantics(payload.Value)
			encoded, err := payload.Value.MachineJSON()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			machineParsed, err := ParseStructured(encoded)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !sameResultSemantics(machineParsed, want) {
				t.Fatalf("machine JSON round trip lost semantics:\n%+v\n%+v", machineParsed, want)
			}

			legacyParsed, err := legacyFromDisplayLines(legacyDisplayLines(payload.Value))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !sameResultSemantics(legacyParsed, want) {
				if legacyRoundTripLoss(payload.Value) == "" {
					t.Fatalf("legacy round trip lost uncategorized semantics:\n%+v\n%+v", legacyParsed, want)
				}
			}
		})
	}
}

func TestProtocolMeasurementNonContractNoise(t *testing.T) {
	noisy := 0
	for _, payload := range realMeasuredPayloads(t) {
		if !hasNonContractNoise(payload.Value) {
			continue
		}
		noisy++
		want := contractSemantics(payload.Value)
		encoded, err := payload.Value.MachineJSON()
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		machineParsed, err := ParseStructured(encoded)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		legacyParsed, err := legacyFromDisplayLines(legacyDisplayLines(payload.Value))
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !sameResultSemantics(machineParsed, want) || !sameResultSemantics(legacyParsed, want) {
			t.Fatalf("%s: 契約外field除外が両形式で一致しません", payload.Name)
		}
	}
	if noisy == 0 {
		t.Fatal("実corpusに契約外field混入payloadがありません: 観測前提が壊れています")
	}
}

func TestProtocolMeasurementLegacyNoneSentinelLoss(t *testing.T) {
	review := passResult()
	review.Targets = []string{noneTargetsSentinel}

	legacyParsed, err := legacyFromDisplayLines(legacyDisplayLines(review))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(legacyParsed.Targets) != 0 {
		t.Fatalf("legacy decoderはnone sentinelを保存しません: %v", legacyParsed.Targets)
	}
	encoded, err := review.MachineJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	machineParsed, err := ParseStructured(encoded)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(machineParsed.Targets) != 1 || machineParsed.Targets[0] != noneTargetsSentinel {
		t.Fatalf("machine JSONはnone sentinelを保存します: %v", machineParsed.Targets)
	}
}

func TestProtocolMeasurementNoiseAndDuplication(t *testing.T) {
	for _, payload := range allMeasuredPayloads(t) {
		machine := measureMachine(payload.Value)
		legacy := measureLegacy(payload.Value)
		if machine.NoiseFields != 0 {
			t.Fatalf("%s: machine JSON noise = %d", payload.Name, machine.NoiseFields)
		}
		if machine.DuplicateValues != 0 || legacy.DuplicateValues != 0 {
			t.Fatalf("%s: duplicate values machine=%d legacy=%d", payload.Name, machine.DuplicateValues, legacy.DuplicateValues)
		}
		wantLegacyNoise := 0
		if len(payload.Value.Artifacts) == 0 {
			wantLegacyNoise++
		}
		if wantLegacyNoise != legacy.NoiseFields {
			t.Fatalf("%s: legacy noise = %d, want %d", payload.Name, legacy.NoiseFields, wantLegacyNoise)
		}
	}
}

func TestProtocolMeasurementByteDeltaBand(t *testing.T) {
	var legacyBytes, machineBytes, legacyRun, machineRun int
	for _, payload := range realMeasuredPayloads(t) {
		legacy := measureLegacy(payload.Value)
		machine := measureMachine(payload.Value)
		legacyBytes += legacy.StdoutBytes
		machineBytes += machine.StdoutBytes
		legacyRun += legacy.TokensRun
		machineRun += machine.TokensRun
	}
	deltaBytes := float64(machineBytes-legacyBytes) / float64(legacyBytes)
	deltaRun := float64(machineRun-legacyRun) / float64(legacyRun)
	if deltaBytes < -0.02 || deltaBytes > 0.05 {
		t.Fatalf("stdout bytes delta = %.3f (legacy=%d machine=%d)", deltaBytes, legacyBytes, machineBytes)
	}
	if deltaRun < -0.02 || deltaRun > 0.05 {
		t.Fatalf("token proxy delta = %.3f (legacy=%d machine=%d)", deltaRun, legacyRun, machineRun)
	}
}
