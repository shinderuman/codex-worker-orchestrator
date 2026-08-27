package packet

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type requiredFieldCase struct {
	key   string
	blank func(*Result)
	want  string

	mismatch bool
}

type statusContract struct {
	name     string
	valid    Result
	validate func(Result) error
	required []requiredFieldCase
}

type targetsElementCase struct {
	name    string
	targets []string
	accept  bool

	wantRejectSubstring string
}

const (
	workerStructuredFixture   = `{"status":"IMPLEMENTED","risk":"LOW","summary":"s","requirement_coverage":"c","tests":"t","unverified":"none","targets":[],"artifacts":[]}`
	reviewerStructuredFixture = `{"status":"PASS","risk":"LOW","summary":"s","requirement_coverage":"c","invariants":"i","test_evidence":"e","issues":"none","residual_risk":"none","targets":["a.go:f"],"artifacts":[]}`
)

func statusContracts() []statusContract {
	worker := ValidateWorkerResult
	reviewer := ValidateReviewerResult
	return []statusContract{
		{
			name:     "worker IMPLEMENTED",
			valid:    implementedResult(),
			validate: worker,
			required: []requiredFieldCase{
				{"STATUS", func(r *Result) { r.Status = "" }, "worker結果のstatus", true},
				{"RISK", func(r *Result) { r.Risk = "" }, "LOWまたはHIGH", false},
				{"SUMMARY", func(r *Result) { r.Summary = " " }, "必須field summary", false},
				{"REQUIREMENT_COVERAGE", func(r *Result) { r.RequirementCoverage = "" }, "必須field requirement_coverage", false},
				{"TESTS", func(r *Result) { r.Tests = "" }, "必須field tests", false},
				{"UNVERIFIED", func(r *Result) { r.Unverified = "" }, "必須field unverified", false},
			},
		},
		{
			name: "worker NEEDS_SOL_DECISION",
			valid: Result{
				Status:          StatusNeedsSolDecision,
				Risk:            RiskHigh,
				Decision:        "d",
				Evidence:        "e",
				Options:         "o",
				Recommendation:  "r",
				TestObligations: "t",
				Targets:         []string{"glm-worker/internal/packet/validate.go:ValidateWorkerResult"},
			},
			validate: worker,
			required: []requiredFieldCase{
				{"STATUS", func(r *Result) { r.Status = "" }, "worker結果のstatus", true},
				{"RISK", func(r *Result) { r.Risk = RiskLow }, "NEEDS_SOL_DECISIONのrisk", false},
				{"DECISION", func(r *Result) { r.Decision = "" }, "必須field decision", false},
				{"EVIDENCE", func(r *Result) { r.Evidence = "" }, "必須field evidence", false},
				{"OPTIONS", func(r *Result) { r.Options = "" }, "必須field options", false},
				{"RECOMMENDATION", func(r *Result) { r.Recommendation = "" }, "必須field recommendation", false},
				{"TEST_OBLIGATIONS", func(r *Result) { r.TestObligations = "" }, "必須field test_obligations", false},
				{"TARGETS", func(r *Result) { r.Targets = nil }, "NEEDS_SOL_DECISIONのTARGETSは空", false},
			},
		},
		{
			name:     "reviewer PASS",
			valid:    passResult(),
			validate: reviewer,
			required: []requiredFieldCase{
				{"STATUS", func(r *Result) { r.Status = "" }, "reviewer結果のstatus", true},
				{"RISK", func(r *Result) { r.Risk = RiskHigh }, "PASSのrisk", false},
				{"SUMMARY", func(r *Result) { r.Summary = "" }, "必須field summary", false},
				{"REQUIREMENT_COVERAGE", func(r *Result) { r.RequirementCoverage = "" }, "必須field requirement_coverage", false},
				{"INVARIANTS", func(r *Result) { r.Invariants = "" }, "必須field invariants", false},
				{"TEST_EVIDENCE", func(r *Result) { r.TestEvidence = "" }, "必須field test_evidence", false},
				{"ISSUES", func(r *Result) { r.Issues = "" }, "必須field issues", false},
				{"RESIDUAL_RISK", func(r *Result) { r.ResidualRisk = "" }, "必須field residual_risk", false},
				{"TARGETS", func(r *Result) { r.Targets = nil }, "PASSのTARGETSは空", false},
			},
		},
		{
			name: "reviewer FIX_REQUIRED",
			valid: func() Result {
				fix := passResult()
				fix.Status = StatusFixRequired
				fix.Risk = RiskHigh
				return fix
			}(),
			validate: reviewer,
			required: []requiredFieldCase{
				{"STATUS", func(r *Result) { r.Status = "" }, "reviewer結果のstatus", true},
				{"RISK", func(r *Result) { r.Risk = "" }, "LOWまたはHIGH", false},
				{"SUMMARY", func(r *Result) { r.Summary = "" }, "必須field summary", false},
				{"REQUIREMENT_COVERAGE", func(r *Result) { r.RequirementCoverage = "" }, "必須field requirement_coverage", false},
				{"INVARIANTS", func(r *Result) { r.Invariants = "" }, "必須field invariants", false},
				{"TEST_EVIDENCE", func(r *Result) { r.TestEvidence = "" }, "必須field test_evidence", false},
				{"ISSUES", func(r *Result) { r.Issues = "" }, "必須field issues", false},
				{"RESIDUAL_RISK", func(r *Result) { r.ResidualRisk = "" }, "必須field residual_risk", false},
				{"TARGETS", func(r *Result) { r.Targets = nil }, "FIX_REQUIREDのTARGETSは空", false},
			},
		},
		{
			name: "reviewer NEEDS_SOL_REVIEW",
			valid: func() Result {
				review := passResult()
				review.Status = StatusNeedsSolReview
				review.Risk = RiskHigh
				review.SolQuestion = "q"
				return review
			}(),
			validate: reviewer,
			required: []requiredFieldCase{
				{"STATUS", func(r *Result) { r.Status = "" }, "reviewer結果のstatus", true},
				{"RISK", func(r *Result) { r.Risk = RiskLow }, "NEEDS_SOL_REVIEWのrisk", false},
				{"SUMMARY", func(r *Result) { r.Summary = "" }, "必須field summary", false},
				{"REQUIREMENT_COVERAGE", func(r *Result) { r.RequirementCoverage = "" }, "必須field requirement_coverage", false},
				{"INVARIANTS", func(r *Result) { r.Invariants = "" }, "必須field invariants", false},
				{"TEST_EVIDENCE", func(r *Result) { r.TestEvidence = "" }, "必須field test_evidence", false},
				{"ISSUES", func(r *Result) { r.Issues = "" }, "必須field issues", false},
				{"RESIDUAL_RISK", func(r *Result) { r.ResidualRisk = "" }, "必須field residual_risk", false},
				{"TARGETS", func(r *Result) { r.Targets = nil }, "TARGETSは空", false},
				{"SOL_QUESTION", func(r *Result) { r.SolQuestion = "" }, "必須field sol_question", false},
			},
		},
	}
}

func TestStatusRequiredFieldsCorrespondence(t *testing.T) {
	for _, contract := range statusContracts() {
		t.Run(contract.name, func(t *testing.T) {
			if err := contract.validate(contract.valid); err != nil {
				t.Fatalf("positive result rejected: %v", err)
			}
			for _, field := range contract.required {
				t.Run(field.key, func(t *testing.T) {
					blanked := contract.valid
					field.blank(&blanked)
					err := contract.validate(blanked)
					if err == nil {
						t.Fatalf("%sを空にしても受理されました", field.key)
					}
					if field.mismatch && !IsMismatchError(err) {
						t.Fatalf("error must be mismatch, got %v", err)
					}
					if !field.mismatch && !IsConstraintError(err) {
						t.Fatalf("error must be constraint, got %v", err)
					}
					if !strings.Contains(err.Error(), field.want) {
						t.Fatalf("err = %q, want substring %q", err.Error(), field.want)
					}
				})
			}
		})
	}
}

func TestWorkerDecisionTargetsNoneSentinel(t *testing.T) {
	decision := Result{
		Status:          StatusNeedsSolDecision,
		Risk:            RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "t",
		Targets:         []string{"none"},
	}
	if err := ValidateWorkerResult(decision); err != nil {
		t.Fatalf("予約値none要素のNEEDS_SOL_DECISIONは旧契約どおり有効: %v", err)
	}
}

func sharedTargetsElementCases() []targetsElementCase {
	return []targetsElementCase{
		{name: "empty element", targets: []string{""}, accept: false, wantRejectSubstring: "空・空白のみ"},
		{name: "whitespace element", targets: []string{"   "}, accept: false, wantRejectSubstring: "空・空白のみ"},
		{name: "blank among concrete", targets: []string{"a.go:10", " "}, accept: false, wantRejectSubstring: "空・空白のみ"},
		{name: "concrete single", targets: []string{"glm-worker/internal/packet/validate.go:validateTargets"}, accept: true},
		{name: "concrete multiple", targets: []string{"a.go:10", "b.go:20"}, accept: true},
		{name: "padded concrete", targets: []string{" a.go:10 "}, accept: true},
		{name: "duplicate", targets: []string{"a.go:10", "a.go:10"}, accept: false, wantRejectSubstring: "重複"},
		{name: "duplicate after trim", targets: []string{"a.go:10", " a.go:10"}, accept: false, wantRejectSubstring: "重複"},

		{name: "duplicate artifacts-like element", targets: []string{"artifact.go:10", "artifact.go:10"}, accept: false, wantRejectSubstring: "重複"},
		{name: "mixed none", targets: []string{"none", "glm-worker/internal/foo.go:10"}, accept: false, wantRejectSubstring: "混在"},
		{name: "none case variant sole", targets: []string{"NONE"}, accept: false, wantRejectSubstring: "厳密表現"},
		{name: "none title case variant", targets: []string{"None"}, accept: false, wantRejectSubstring: "厳密表現"},
		{name: "padded none", targets: []string{" none "}, accept: false, wantRejectSubstring: "厳密表現"},
		{name: "mixed none case variant", targets: []string{"glm-worker/internal/foo.go:10", "NONE"}, accept: false, wantRejectSubstring: "厳密表現"},
	}
}

func TestTargetsElementAcceptanceByStatus(t *testing.T) {
	validTargets := []string{"glm-worker/internal/packet/validate.go:validateTargets"}
	statuses := []struct {
		name      string
		valid     Result
		validate  func(Result) error
		empty     bool
		noneSole  bool
		packetFix bool
	}{
		{name: "worker IMPLEMENTED", valid: implementedResult(), validate: ValidateWorkerResult, empty: true, noneSole: true},
		{
			name: "worker NEEDS_SOL_DECISION",
			valid: Result{
				Status:          StatusNeedsSolDecision,
				Risk:            RiskHigh,
				Decision:        "d",
				Evidence:        "e",
				Options:         "o",
				Recommendation:  "r",
				TestObligations: "t",
				Targets:         validTargets,
			},
			validate: ValidateWorkerResult,
			noneSole: true,
		},
		{name: "reviewer PASS", valid: passResult(), validate: ValidateReviewerResult, noneSole: true},
		{
			name: "reviewer FIX_REQUIRED",
			valid: func() Result {
				fix := passResult()
				fix.Status = StatusFixRequired
				fix.Risk = RiskHigh
				return fix
			}(),
			validate:  ValidateReviewerResult,
			noneSole:  true,
			packetFix: true,
		},
		{
			name: "reviewer NEEDS_SOL_REVIEW",
			valid: func() Result {
				review := passResult()
				review.Status = StatusNeedsSolReview
				review.Risk = RiskHigh
				review.SolQuestion = "q"
				return review
			}(),
			validate: ValidateReviewerResult,
		},
	}
	for _, status := range statuses {
		t.Run(status.name, func(t *testing.T) {
			if err := status.validate(status.valid); err != nil {
				t.Fatalf("正例が拒否されました: %v", err)
			}
			cases := sharedTargetsElementCases()
			cases = append(cases,
				targetsElementCase{name: "empty array", targets: nil, accept: status.empty, wantRejectSubstring: "TARGETSは空"},
				targetsElementCase{name: "none sole", targets: []string{"none"}, accept: status.noneSole, wantRejectSubstring: "TARGETSはnone"},
				targetsElementCase{name: "PACKET sole", targets: []string{"PACKET"}, accept: status.packetFix, wantRejectSubstring: "PACKET"},
				targetsElementCase{name: "PACKET case variant", targets: []string{"packet"}, accept: false, wantRejectSubstring: "PACKET"},
				targetsElementCase{name: "PACKET mixed", targets: []string{"PACKET", "a.go:10"}, accept: false, wantRejectSubstring: "PACKET"},
			)
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					mutated := status.valid
					mutated.Targets = c.targets
					err := status.validate(mutated)
					if c.accept {
						if err != nil {
							t.Fatalf("受理期待のtargets %qが拒否されました: %v", c.targets, err)
						}
						return
					}
					if err == nil {
						t.Fatalf("拒否期待のtargets %qが受理されました", c.targets)
					}
					if !IsConstraintError(err) {
						t.Fatalf("error must be constraint, got %v", err)
					}
					if !strings.Contains(err.Error(), c.wantRejectSubstring) {
						t.Fatalf("err = %q, want substring %q", err.Error(), c.wantRejectSubstring)
					}
					if category := RejectCategory(err); category != "targets-none" {
						t.Fatalf("category = %q, want targets-none (%v)", category, err)
					}
				})
			}
		})
	}
}

func resultJSONFieldNames(t *testing.T) map[string]bool {
	t.Helper()
	typ := reflect.TypeOf(Result{})
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

func schemaObjectNodes(node map[string]any) []map[string]any {
	nodes := []map[string]any{node}
	if properties, ok := node["properties"].(map[string]any); ok {
		for _, raw := range properties {
			if child, ok := raw.(map[string]any); ok && child["type"] == "object" {
				nodes = append(nodes, schemaObjectNodes(child)...)
			}
		}
	}
	return nodes
}

func schemaPropertyNames(node map[string]any) []string {
	properties, _ := node["properties"].(map[string]any)
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	return names
}

func schemaRequiredNames(t *testing.T, node map[string]any) []string {
	t.Helper()
	rawRequired, _ := node["required"].([]any)
	names := make([]string, 0, len(rawRequired))
	for _, raw := range rawRequired {
		name, ok := raw.(string)
		if !ok {
			t.Fatalf("requiredに非string要素: %v", raw)
		}
		names = append(names, name)
	}
	return names
}

func TestProducerSchemaConsumerAcceptance(t *testing.T) {
	knownFields := resultJSONFieldNames(t)
	for name, build := range map[string]func() (string, error){
		"worker":   WorkerSchemaJSON,
		"reviewer": ReviewerSchemaJSON,
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := build()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			var schema map[string]any
			if err := json.Unmarshal([]byte(encoded), &schema); err != nil {
				t.Fatalf("schema json: %v", err)
			}
			for _, node := range schemaObjectNodes(schema) {
				if _, ok := node["additionalProperties"]; ok {
					t.Fatal("schemaは未検証語彙のadditionalPropertiesを使ってはいけません")
				}
				for _, property := range schemaPropertyNames(node) {
					if !knownFields[property] {
						t.Fatalf("schema property %qはconsumerのResult fieldへ対応していません", property)
					}
				}
			}
		})
	}

	t.Run("reviewer required mirrors old presence contract", func(t *testing.T) {
		encoded, err := ReviewerSchemaJSON()
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(encoded), &schema); err != nil {
			t.Fatalf("schema json: %v", err)
		}
		required := strings.Join(schemaRequiredNames(t, schema), ",")
		if required != "status,risk,targets,artifacts" {
			t.Fatalf("reviewer required = %q, want status,risk,targets,artifacts", required)
		}
	})
	t.Run("worker required mirrors old presence contract", func(t *testing.T) {
		encoded, err := WorkerSchemaJSON()
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(encoded), &schema); err != nil {
			t.Fatalf("schema json: %v", err)
		}
		required := strings.Join(schemaRequiredNames(t, schema), ",")
		if required != "status,risk,targets,artifacts" {
			t.Fatalf("worker required = %q, want status,risk,targets,artifacts", required)
		}
	})
}

func TestParseStructuredIgnoresSchemaPermittedUnknownFields(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		validate func(Result) error
	}{
		{"worker top level", workerStructuredFixture, ValidateWorkerResult},
		{"reviewer top level", reviewerStructuredFixture, ValidateReviewerResult},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := strings.Replace(c.base, "{", `{"untracked_field":"value",`, 1)
			result, err := ParseStructured([]byte(data))
			if err != nil {
				t.Fatalf("schema許容の未知fieldで拒否されました: %v", err)
			}
			if err := c.validate(result); err != nil {
				t.Fatalf("意味検証不合格: %v", err)
			}
			emitted, err := result.MachineJSON()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(string(emitted), "untracked_field") {
				t.Fatalf("未知fieldがmachine JSONへ伝播しています: %s", emitted)
			}
		})
	}
}

func TestParseStructuredKeepsKnownFieldStrictness(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"string field as number", `{"status":"IMPLEMENTED","risk":"LOW","summary":3,"artifacts":[]}`},
		{"array field as string", `{"status":"IMPLEMENTED","risk":"LOW","targets":"a.go","artifacts":[]}`},
		{"array item as object", `{"status":"IMPLEMENTED","risk":"LOW","targets":[{"file":"a.go"}],"artifacts":[]}`},
		{"risk as number", `{"status":"IMPLEMENTED","risk":1,"artifacts":[]}`},
		{"trailing garbage", `{"status":"IMPLEMENTED","risk":"LOW","artifacts":[]} trailing`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := ParseStructured([]byte(c.data))
			if err == nil {
				t.Fatalf("expected mismatch error, got %+v", result)
			}
			if !IsMismatchError(err) {
				t.Fatalf("error must be mismatch, got %v", err)
			}
		})
	}
}

func TestConsumerBackstopForReviewerTargetsKey(t *testing.T) {
	data := strings.Replace(reviewerStructuredFixture, `"targets":["a.go:f"],`, "", 1)
	result, err := ParseStructured([]byte(data))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	err = ValidateReviewerResult(result)
	if err == nil || !IsConstraintError(err) || !strings.Contains(err.Error(), "PASSのTARGETSは空") {
		t.Fatalf("err = %v, want constraint TARGETS拒否", err)
	}
}

func TestConsumerBackstopForWorkerTargetsKey(t *testing.T) {
	decision := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","artifacts":[]}`
	result, err := ParseStructured([]byte(decision))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	err = ValidateWorkerResult(result)
	if err == nil || !IsConstraintError(err) || !strings.Contains(err.Error(), "NEEDS_SOL_DECISIONのTARGETSは空") {
		t.Fatalf("err = %v, want constraint TARGETS拒否", err)
	}
	empty := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":[],"artifacts":[]}`
	parsed, err := ParseStructured([]byte(empty))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	err = ValidateWorkerResult(parsed)
	if err == nil || !IsConstraintError(err) {
		t.Fatalf("err = %v, want constraint TARGETS拒否", err)
	}
	implemented, err := ParseStructured([]byte(workerStructuredFixture))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateWorkerResult(implemented); err != nil {
		t.Fatalf("IMPLEMENTEDの空targetsは旧契約どおり有効: %v", err)
	}
}

func TestStructuredStatusPositives(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		validate func(Result) error
	}{
		{"IMPLEMENTED", workerStructuredFixture, ValidateWorkerResult},
		{"NEEDS_SOL_DECISION", `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":["none"],"artifacts":[]}`, ValidateWorkerResult},
		{"PASS", reviewerStructuredFixture, ValidateReviewerResult},
		{"FIX_REQUIRED", `{"status":"FIX_REQUIRED","risk":"HIGH","summary":"s","requirement_coverage":"c","invariants":"i","test_evidence":"e","issues":"i","residual_risk":"r","targets":["a.go:f"],"artifacts":[]}`, ValidateReviewerResult},
		{"NEEDS_SOL_REVIEW", `{"status":"NEEDS_SOL_REVIEW","risk":"HIGH","summary":"s","requirement_coverage":"c","invariants":"i","test_evidence":"e","issues":"i","residual_risk":"r","targets":["a.go:f"],"artifacts":[],"sol_question":"q"}`, ValidateReviewerResult},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, err := ParseStructured([]byte(c.data))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if err := c.validate(parsed); err != nil {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
