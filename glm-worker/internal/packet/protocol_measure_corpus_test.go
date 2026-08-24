package packet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tokenProxyRunBasedはBPE風の決定論token proxy。ASCII英数連続を1 token、
// ASCII区切記号の連続を1 token、非ASCII rune(日本語等)を1 tokenずつとして数える。
// JSONの句読点がkey名・引用符で増える効果を、実tokenizerの概算として拾う。
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

// tokenProxyCharPunctは悲観側の決定論token proxy。ASCII区切記号と非ASCII runeを
// 1文字ずつ別tokenとして数え、JSON構文の句読点増加が最悪どう読まれるかを示す。
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

// semanticValueBytesは両形式が同じ値として運ぶ意味本文のbyte数。契約text field
// 全体とtargets/artifacts各要素だけを数え、key名・区切り・引用符・placeholderは
// 含めない。structured bytes = stdout bytes - この値。
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

// renderMeasurementは1件のsemantic payloadを1形式へrenderした結果の測定値。
type renderMeasurement struct {
	Format          string
	StdoutBytes     int
	TokensRun       int
	TokensCharPunct int
	SemanticBytes   int
	StructuredBytes int
	// NoiseFieldsは意味本文を持たない行・keyの数。旧形式の`none` placeholder・
	// 空text field行に相当し、machine JSONのomitemptyは常に0。
	NoiseFields int
	// DuplicateValuesは同じ値が意味的に別field・要素へ重複出力された回数。
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

// addValuesは意味重複検出のため、status/risk/text field/要素の空でない値の出現数を数える。
// 予約placeholder(none/PACKET)はprotocol語彙で意味本文ではないため数えない。
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

// measureLegacyNoiseは旧形式が意味本文なしで出力した行数を数える。空text fieldの
// 行と、配列が空のまま`none` placeholderだけを出したTARGETS/ARTIFACTS行。
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

// sameResultSemanticsはstatus/risk/全契約text field/targets/artifactsの意味等価比較。
// nil配列と空配列は同じ「要素なし」として扱う。
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

// contractSemanticsはstatus別契約field・status/risk・targets/artifactsだけを残した
// 投影。実corpusにはproducerが契約外field(例: IMPLEMENTEDへのdecision filler)を
// 混入させる場合があり、machine JSON・旧KEY行形式とも契約面だけを運ぶため、
// 保持比較はこの投影に対して行う。契約外fieldの混入数自体は測定値として報告する。
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

// hasNonContractNoiseはproducerが契約外fieldへ値を混入したpayloadかを判定する。
func hasNonContractNoise(value Result) bool {
	return !sameResultSemantics(value, contractSemantics(value))
}

// measuredPayloadは測定corpus 1件。Sourceは実telemetry由来の由来情報。
type measuredPayload struct {
	Name   string
	Source string
	Value  Result
}

// syntheticMeasuredPayloadsは契約境界を含む固定payload。全status正例・上限長
// field・none sentinel・report-only PACKET・要素内セミコロンの旧形式脆弱性。
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

// realMeasuredPayloadsは保存telemetryから収穫した受理済みstructured結果。
// testdata出所とeraはtest file本文で固定し、収穫は読み取り専用で行った。
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

// legacyRoundTripExpectedLossは旧形式decoderが往復で失う既知の意味。reviewerの
// TARGETS予約値noneは`none` placeholderへ潰れて要素なしになり、要素内セミコロンは
// 要素分割を起こす。それ以外の契約fieldは往復保存される。
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

// TestProtocolMeasurementSemanticRetentionは同じsemantic payloadを両形式へ
// renderしてdecodeしたとき、Codexが受け取る意味情報が保存されるかを固定する。
// 比較面はstatus別契約field・status/risk・targets/artifacts。machine JSONは全corpus
// で無劣化、旧KEY行形式は列挙した既知喪失のみ許容する。
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

// TestProtocolMeasurementNonContractNoiseは実corpusに契約外fieldを混入したpayloadが
// 存在することと、両形式がその混入を同じように契約面から除外することを固定する。
// 混入fieldの値自体はどちらの形式でもCodexへ届かない。
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

// TestProtocolMeasurementLegacyNoneSentinelLossは旧形式だけが持つ意味喪失を実例で
// 固定する。reviewer TARGETS予約値none(現契約で有効)は旧形式では`TARGETS: none`
// placeholderへ描画され、decoderが要素なしへ潰す。machine JSONは配列として保存する。
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

// TestProtocolMeasurementNoiseAndDuplicationは意味本文のない出力(placeholder・空行)
// と値重複の測定を固定する。machine JSONのnoiseは構造的に0、旧形式はartifacts空の
// payloadで`ARTIFACTS: none`行が残る。値重複は両形式とも発生しない。
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

// TestProtocolMeasurementByteDeltaBandは実payload corpusでのbyte/token差を測定値
// どおり固定する。machine JSONはkey名・引用符の構文費用で旧形式よりわずかに大きく
// なる(-2%..+5%の帯)ことが測定結論であり、形式の優劣を前提にしない。
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
