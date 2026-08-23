// Package packetはstructured outputのtyped結果と、Sol表示への変換・意味検証を担う。
// model呼出の唯一のprotocolは--json-schemaで強制されるtyped structured outputであり、
// marker抽出・KEY行parser・重複/迷子marker検出のようなtext構造検査は持たない。
package packet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 表示・検証契約の上限。structured outputでは物理行数は意味を持たないため、
// 親Codexへ出すmachine protocol全体のbyte上限と1 field本文のbyte上限だけを
// 圧縮規律として残す。
const (
	MaxPacketBytes     = 6 * 1024
	MaxFieldBytes      = 1536
	MaxDiagnosticBytes = 6 * 1024
)

// Statusはworkflow終端の種別。worker roleとreviewer roleで許容集合が異なる。
type Status string

const (
	StatusImplemented      Status = "IMPLEMENTED"
	StatusNeedsSolDecision Status = "NEEDS_SOL_DECISION"
	StatusPass             Status = "PASS"
	StatusFixRequired      Status = "FIX_REQUIRED"
	StatusNeedsSolReview   Status = "NEEDS_SOL_REVIEW"
)

// RiskはSol判断へ昇格すべき変更かの申告。意味整合はValidate*Resultが強制する。
type Risk string

const (
	RiskLow  Risk = "LOW"
	RiskHigh Risk = "HIGH"
)

// ReportOnlyTargetsはFIX_REQUIREDのTARGETS予約値。reviewerがコード・diffを正しいと
// 確認し報告の意味情報だけを不足と指摘するときに使い、productionは実装修正と
// 報告再出力をこの値だけで機械識別する。
const ReportOnlyTargets = "PACKET"

// noneTargetsSentinelは「対象が概念的でfile targetがない」ことを表す旧WORKER.md
// 「不要ならnone」由来のTARGETS予約値。小文字厳密表現の単独要素としてだけ使用できる。
const noneTargetsSentinel = "none"

// Resultは1回のmodel呼出が返すtyped結果。worker/reviewer両roleで同じ構造を持ち、
// statusenumと意味検証でrole契約を強制する。未知の意味問題は各free text fieldへ
// 残り、構造はschemaとこの型が固定する。
//
// status別契約(machine protocol・検証・人間向けprojectionの共通table):
//
//	status              role      risk      必須text field                                                         targets受理
//	IMPLEMENTED         worker    LOW/HIGH  summary, requirement_coverage, tests, unverified                       空配列可 / none可 / PACKET不可
//	NEEDS_SOL_DECISION  worker    HIGH      decision, evidence, options, recommendation, test_obligations         空配列不可 / none可 / PACKET不可
//	PASS                reviewer  LOW       summary, requirement_coverage, invariants, test_evidence,             空配列不可 / none可 / PACKET不可
//	                                     issues, residual_risk
//	FIX_REQUIRED        reviewer  LOW/HIGH  PASSと同上                                                             空配列不可 / none可 / PACKET単独可(報告再出力専用)
//	NEEDS_SOL_REVIEW    reviewer  HIGH      PASSと同上 + sol_question                                              空配列不可 / none不可 / PACKET不可
//
// 共通: 契約text fieldは空・改行・1536 bytes超を拒否する。targets/artifacts各要素は
// 改行不可・1536 bytes以内・TrimSpace後の重複不可。artifactsは空配列可で、指定時は
// task専用artifact dir配下の実在通常fileの絶対pathだけを取る。結果全体はmachine JSONで
// 6144 bytes以内。契約外のstatus・fieldはmachine protocolへ出力しない。
type Result struct {
	Status              Status   `json:"status"`
	Risk                Risk     `json:"risk"`
	Summary             string   `json:"summary,omitempty"`
	RequirementCoverage string   `json:"requirement_coverage,omitempty"`
	Tests               string   `json:"tests,omitempty"`
	Unverified          string   `json:"unverified,omitempty"`
	Decision            string   `json:"decision,omitempty"`
	Evidence            string   `json:"evidence,omitempty"`
	Options             string   `json:"options,omitempty"`
	Recommendation      string   `json:"recommendation,omitempty"`
	TestObligations     string   `json:"test_obligations,omitempty"`
	Invariants          string   `json:"invariants,omitempty"`
	TestEvidence        string   `json:"test_evidence,omitempty"`
	Issues              string   `json:"issues,omitempty"`
	ResidualRisk        string   `json:"residual_risk,omitempty"`
	SolQuestion         string   `json:"sol_question,omitempty"`
	Targets             []string `json:"targets,omitempty"`
	Artifacts           []string `json:"artifacts,omitempty"`
}

// mismatchErrorはresult event契約・schema適合の破綻。modelの内容修正で回復できない
// 経路のため再依頼せずfail closedする。
type mismatchError struct {
	reason string
}

func (e *mismatchError) Error() string {
	return e.reason
}

// IsMismatchErrorはschema/result契約ミスマッチ(true)と意味検証不合格(false)を区別する。
func IsMismatchError(err error) bool {
	var target *mismatchError
	return errors.As(err, &target)
}

// ParseStructuredはresult eventのauthoritative structured_outputをtyped結果へ変換する。
// producer schemaはadditionalProperties未検証の語彙制限から未知propertyを許容するため、
// decoderも未知fieldを無害に無視して表示・stateへ伝播させない。既知fieldの型不一致と
// status欠落だけを契約ミスマッチとしてfail closedに分類し、必須性・status別意味制約は
// Validate*Resultが厳格に強制する。
func ParseStructured(data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 || string(bytes.TrimSpace(data)) == "null" {
		return Result{}, &mismatchError{reason: "result eventにstructured_outputがありません"}
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, &mismatchError{reason: fmt.Sprintf("structured_outputをResultへ解析できません: %v", err)}
	}
	if result.Status == "" {
		return Result{}, &mismatchError{reason: "structured_outputのstatusが空です"}
	}
	return result, nil
}

// displayFieldはtyped field名を表示KEYへ対応付ける。表示順はrender順で固定する。
type displayField struct {
	key   string
	value string
}

// contractFieldはstatus別契約text fieldの共通定義。machineはschema語彙と同じJSON key、
// displayは旧PACKET表示のKEY。意味検証・machine protocol・人間向けprojectionの3面が
// 同一のstatus別集合を共有し、契約外fieldの機械出力混入を構造的に防ぐ。
type contractField struct {
	machine string
	display string
	value   func(Result) string
}

var implementedContractFields = []contractField{
	{"summary", "SUMMARY", func(r Result) string { return r.Summary }},
	{"requirement_coverage", "REQUIREMENT_COVERAGE", func(r Result) string { return r.RequirementCoverage }},
	{"tests", "TESTS", func(r Result) string { return r.Tests }},
	{"unverified", "UNVERIFIED", func(r Result) string { return r.Unverified }},
}

var needsSolDecisionContractFields = []contractField{
	{"decision", "DECISION", func(r Result) string { return r.Decision }},
	{"evidence", "EVIDENCE", func(r Result) string { return r.Evidence }},
	{"options", "OPTIONS", func(r Result) string { return r.Options }},
	{"recommendation", "RECOMMENDATION", func(r Result) string { return r.Recommendation }},
	{"test_obligations", "TEST_OBLIGATIONS", func(r Result) string { return r.TestObligations }},
}

var reviewerContractFields = []contractField{
	{"summary", "SUMMARY", func(r Result) string { return r.Summary }},
	{"requirement_coverage", "REQUIREMENT_COVERAGE", func(r Result) string { return r.RequirementCoverage }},
	{"invariants", "INVARIANTS", func(r Result) string { return r.Invariants }},
	{"test_evidence", "TEST_EVIDENCE", func(r Result) string { return r.TestEvidence }},
	{"issues", "ISSUES", func(r Result) string { return r.Issues }},
	{"residual_risk", "RESIDUAL_RISK", func(r Result) string { return r.ResidualRisk }},
}

// needsSolReviewContractFieldsはreviewer共通fieldへsol_questionを加えた集合。
// sol_questionはNEEDS_SOL_REVIEWだけの契約fieldで、PASS/FIX_REQUIREDへmodelが混入させた
// 値は検証対象にならずmachine JSON・projectionのどちらにも出ない
// (field audit実測: PASS 10件中1件の混入)。reviewerContractFieldsへ直接appendすると
// 共用backing arrayを伸ばすため、複製へ足す。
var needsSolReviewContractFields = append(append([]contractField{}, reviewerContractFields...),
	contractField{"sol_question", "SOL_QUESTION", func(r Result) string { return r.SolQuestion }})

// contractFieldsはstatus別の契約text field集合。validator・machine protocol・
// 人間向けprojectionが参照する唯一のstatus→field対応。
func (r Result) contractFields() []contractField {
	switch r.Status {
	case StatusImplemented:
		return implementedContractFields
	case StatusNeedsSolDecision:
		return needsSolDecisionContractFields
	case StatusNeedsSolReview:
		return needsSolReviewContractFields
	default:
		return reviewerContractFields
	}
}

// displayFieldsはstatus別の表示field順。契約text fieldはcontractFieldsの集合・順序で
// 並べ、targets/artifactsは配列をセミコロン区切りへ直す。IMPLEMENTEDだけ旧表示どおり
// 空targetsの行を出さない。
func (r Result) displayFields() []displayField {
	fields := []displayField{
		{key: "STATUS", value: string(r.Status)},
		{key: "RISK", value: string(r.Risk)},
	}
	for _, field := range r.contractFields() {
		fields = append(fields, displayField{key: field.display, value: field.value(r)})
	}
	switch r.Status {
	case StatusImplemented:
		if len(r.Targets) > 0 {
			fields = append(fields, r.targetsField())
		}
		fields = append(fields, r.artifactsField())
	default:
		fields = append(fields, r.targetsField(), r.artifactsField())
	}
	return fields
}

func (r Result) targetsField() displayField {
	return displayField{key: "TARGETS", value: joinDisplayList(r.Targets)}
}

func (r Result) artifactsField() displayField {
	return displayField{key: "ARTIFACTS", value: joinDisplayList(r.Artifacts)}
}

func joinDisplayList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ";")
}

// DisplayLinesはSolへ出力する表示行を返す。
func (r Result) DisplayLines() []string {
	fields := r.displayFields()
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		lines = append(lines, field.key+": "+field.value)
	}
	return lines
}

// Displayは人間向け診断projectionの表示行を改行接続した文字列を返す。
// machine protocol(MachineJSON)とは分離されており、最終stdout・prompt埋め込み・
// state保存の機械経路では使わない。旧text PACKET形式のencoderとして
// FromDisplayLines(v2 resume checkpoint読込)の対で保持し、形式の対応関係を
// 往復検証可能に保つ。
func (r Result) Display() string {
	return strings.Join(r.DisplayLines(), "\n")
}

// MachineJSONは親Codexと次のmodel呼出へ出すcompact machine protocol。status別契約
// fieldだけを含め、契約外field・空field・空配列のkeyを出さず、
// 空配列はkey自体を省く(absence = none)。pretty print・HTML escape・改行を含まない
// 1行を返し、keyはschema語彙と共通のためCodexが再解釈なしで構造を読める。
func (r Result) MachineJSON() ([]byte, error) {
	object := map[string]any{
		"status": string(r.Status),
		"risk":   string(r.Risk),
	}
	for _, field := range r.contractFields() {
		if value := field.value(r); value != "" {
			object[field.machine] = value
		}
	}
	if len(r.Targets) > 0 {
		object["targets"] = r.Targets
	}
	if len(r.Artifacts) > 0 {
		object["artifacts"] = r.Artifacts
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// ByteSizeはmachine protocol全体のbyte数。圧縮規律の検証に使う。
// 値型がstring/[]stringだけのためencode失敗は到達せず、失敗時の0が
// MaxPacketBytes検証を素通りさせる経路は実在しない。
func (r Result) ByteSize() int {
	data, err := r.MachineJSON()
	if err != nil {
		return 0
	}
	return len(data)
}

// FromDisplayLinesは旧text PACKET形式で保存されたresume checkpointのworker報告を
// typed結果へ変換する。v2 checkpointのupgrade互換のためだけに存在し、
// model出力の受理経路には使わない。
func FromDisplayLines(lines []string) (Result, error) {
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
	if targets := splitDisplayList(fields["TARGETS"]); len(targets) > 0 {
		result.Targets = targets
	}
	if artifacts := splitDisplayList(fields["ARTIFACTS"]); len(artifacts) > 0 {
		result.Artifacts = artifacts
	}
	return result, nil
}

// splitDisplayListは表示のセミコロン区切りを配列へ戻す。"none"・空は要素なし扱い。
func splitDisplayList(value string) []string {
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
