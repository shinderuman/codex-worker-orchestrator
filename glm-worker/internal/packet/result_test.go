package packet

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseStructuredAcceptsTypedResult(t *testing.T) {
	raw := `{"status":"IMPLEMENTED","risk":"LOW","summary":"s","requirement_coverage":"c","tests":"t","unverified":"none","targets":[],"artifacts":[]}`
	result, err := ParseStructured([]byte(raw))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Status != StatusImplemented || result.Risk != RiskLow {
		t.Fatalf("status/risk = %s/%s", result.Status, result.Risk)
	}
	if result.Summary != "s" || result.Tests != "t" {
		t.Fatalf("fields not parsed: %+v", result)
	}
}

func TestParseStructuredRejectsContractBreaks(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"empty", "   "},
		{"null", "null"},
		{"bad json", `{"status":`},
		{"missing status", `{"risk":"LOW","artifacts":[]}`},
		{"wrong type", `{"status":"IMPLEMENTED","risk":"LOW","targets":"not-array","artifacts":[]}`},
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
			if IsConstraintError(err) {
				t.Fatalf("mismatch must not be constraint, got %v", err)
			}
		})
	}
}

func implementedResult() Result {
	return Result{
		Status:              StatusImplemented,
		Risk:                RiskLow,
		Summary:             "s",
		RequirementCoverage: "c",
		Tests:               "t",
		Unverified:          "none",
	}
}

func passResult() Result {
	return Result{
		Status:              StatusPass,
		Risk:                RiskLow,
		Summary:             "s",
		RequirementCoverage: "c",
		Invariants:          "i",
		TestEvidence:        "e",
		Issues:              "none",
		ResidualRisk:        "none",
		Targets:             []string{"a.go:f"},
	}
}

func TestValidateWorkerResultStatuses(t *testing.T) {
	decision := Result{
		Status:          StatusNeedsSolDecision,
		Risk:            RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "t",

		Targets: []string{"none"},
	}
	if err := ValidateWorkerResult(implementedResult()); err != nil {
		t.Fatalf("implemented: %v", err)
	}
	if err := ValidateWorkerResult(decision); err != nil {
		t.Fatalf("decision: %v", err)
	}

	high := implementedResult()
	high.Risk = RiskHigh
	if err := ValidateWorkerResult(high); err != nil {
		t.Fatalf("implemented high risk: %v", err)
	}
}

func TestValidateWorkerResultRejections(t *testing.T) {
	cases := []struct {
		name           string
		mutate         func(*Result)
		want           string
		schemaMismatch bool
	}{

		{"reviewer status", func(r *Result) { r.Status = StatusPass }, "worker結果のstatus", true},
		{"decision low risk", func(r *Result) {
			r.Status = StatusNeedsSolDecision
			r.Risk = RiskLow
		}, "NEEDS_SOL_DECISIONのrisk", false},
		{"decision no targets", func(r *Result) {
			r.Status = StatusNeedsSolDecision
			r.Risk = RiskHigh
			r.Decision = "d"
			r.Evidence = "e"
			r.Options = "o"
			r.Recommendation = "r"
			r.TestObligations = "t"
		}, "NEEDS_SOL_DECISIONのTARGETSは空", false},
		{"missing summary", func(r *Result) { r.Summary = " " }, "必須field summary", false},
		{"multiline field", func(r *Result) { r.Tests = "line1\nline2" }, "改行", false},
		{"oversize field", func(r *Result) { r.Summary = strings.Repeat("x", MaxFieldBytes+1) }, "bytes以内", false},
		{"unknown risk", func(r *Result) { r.Risk = Risk("MEDIUM") }, "LOWまたはHIGH", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := implementedResult()
			c.mutate(&result)
			err := ValidateWorkerResult(result)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if c.schemaMismatch {
				if !IsMismatchError(err) {
					t.Fatalf("error must be mismatch, got %v", err)
				}
			} else if !IsConstraintError(err) {
				t.Fatalf("error must be constraint, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

func TestValidateReviewerResultStatuses(t *testing.T) {
	if err := ValidateReviewerResult(passResult()); err != nil {
		t.Fatalf("pass: %v", err)
	}

	fix := passResult()
	fix.Status = StatusFixRequired
	fix.Risk = RiskHigh
	fix.Targets = []string{"a.go:f"}
	if err := ValidateReviewerResult(fix); err != nil {
		t.Fatalf("fix required: %v", err)
	}

	fixNone := fix
	fixNone.Targets = []string{"none"}
	if err := ValidateReviewerResult(fixNone); err != nil {
		t.Fatalf("fix required with none target: %v", err)
	}

	review := passResult()
	review.Status = StatusNeedsSolReview
	review.Risk = RiskHigh
	review.Targets = []string{"a.go:f"}
	review.SolQuestion = "q"
	if err := ValidateReviewerResult(review); err != nil {
		t.Fatalf("needs sol review: %v", err)
	}
}

func TestValidateReviewerResultRejections(t *testing.T) {
	cases := []struct {
		name           string
		mutate         func(*Result)
		want           string
		schemaMismatch bool
	}{
		{"worker status", func(r *Result) { r.Status = StatusImplemented }, "reviewer結果のstatus", true},
		{"pass high risk", func(r *Result) { r.Risk = RiskHigh }, "PASSのrisk", false},
		{"pass no targets", func(r *Result) { r.Targets = nil }, "PASSのTARGETSは空", false},
		{"fix no targets", func(r *Result) {
			r.Status = StatusFixRequired
			r.Targets = nil
		}, "FIX_REQUIREDのTARGETSは空", false},
		{"sol review low risk", func(r *Result) {
			r.Status = StatusNeedsSolReview
			r.Risk = RiskLow
		}, "NEEDS_SOL_REVIEWのrisk", false},
		{"sol review no targets", func(r *Result) {
			r.Status = StatusNeedsSolReview
			r.Risk = RiskHigh
			r.SolQuestion = "q"
			r.Targets = nil
		}, "TARGETSは空", false},
		{"sol review none target", func(r *Result) {
			r.Status = StatusNeedsSolReview
			r.Risk = RiskHigh
			r.Targets = []string{"none"}
			r.SolQuestion = "q"
		}, "TARGETSはnone", false},
		{"sol review missing question", func(r *Result) {
			r.Status = StatusNeedsSolReview
			r.Risk = RiskHigh
			r.Targets = []string{"a.go"}
		}, "必須field sol_question", false},
		{"missing invariants", func(r *Result) { r.Invariants = "" }, "必須field invariants", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := passResult()
			c.mutate(&result)
			err := ValidateReviewerResult(result)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if c.schemaMismatch {
				if !IsMismatchError(err) {
					t.Fatalf("error must be mismatch, got %v", err)
				}
			} else if !IsConstraintError(err) {
				t.Fatalf("error must be constraint, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

func TestValidateResultRejectsOversizeDisplay(t *testing.T) {
	result := implementedResult()
	result.Summary = strings.Repeat("x", 1520)
	result.RequirementCoverage = strings.Repeat("y", 1520)
	result.Tests = strings.Repeat("z", 1520)
	result.Unverified = strings.Repeat("w", 1520)
	err := ValidateWorkerResult(result)
	if err == nil || !strings.Contains(err.Error(), "結果全体") {
		t.Fatalf("err = %v", err)
	}
}

func TestMachineJSONIsCompactStatusScopedLine(t *testing.T) {
	result := implementedResult()
	result.Targets = []string{"a.go:f", "b.go:g"}
	result.Artifacts = []string{"/tmp/report.md"}
	data, err := result.MachineJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `{"artifacts":["/tmp/report.md"],"requirement_coverage":"c","risk":"LOW","status":"IMPLEMENTED","summary":"s","targets":["a.go:f","b.go:g"],"tests":"t","unverified":"none"}`
	if string(data) != want {
		t.Fatalf("machine JSON =\n%s\nwant\n%s", data, want)
	}
	if strings.Contains(string(data), "\n") {
		t.Fatalf("machine JSON must be single line: %s", data)
	}
	if result.ByteSize() != len(data) {
		t.Fatalf("ByteSize = %d want %d", result.ByteSize(), len(data))
	}

	noise := implementedResult()
	noise.Decision = "none"
	noise.Evidence = "n/a"
	noise.Options = "https://claude.ai/null"
	noise.Recommendation = "r"
	noise.TestObligations = "t"
	noise.SolQuestion = "q"
	data, err = noise.MachineJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(string(data), "decision") || strings.Contains(string(data), "sol_question") ||
		strings.Contains(string(data), "targets") || strings.Contains(string(data), "artifacts") {
		t.Fatalf("契約外field・空配列keyがmachine JSONへ混入: %s", data)
	}

	review := passResult()
	review.Status = StatusNeedsSolReview
	review.Risk = RiskHigh
	review.SolQuestion = "a<b & c>"
	data, err = review.MachineJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(string(data), `"sol_question":"a<b & c>"`) {
		t.Fatalf("sol_questionがHTML escape無しで出力されていない: %s", data)
	}
}

func TestSolQuestionIsNeedsSolReviewOnly(t *testing.T) {
	for _, status := range []Status{StatusPass, StatusFixRequired} {
		result := passResult()
		result.Status = status
		if status == StatusFixRequired {
			result.Risk = RiskHigh
		}

		result.SolQuestion = "line1\nline2" + strings.Repeat("x", MaxFieldBytes)
		if err := ValidateReviewerResult(result); err != nil {
			t.Fatalf("%s: 混入sol_questionがvalidation対象になっています: %v", status, err)
		}
		data, err := result.MachineJSON()
		if err != nil {
			t.Fatalf("%s: err = %v", status, err)
		}
		if strings.Contains(string(data), "sol_question") {
			t.Fatalf("%s: 混入sol_questionがmachine JSONへ流出: %s", status, data)
		}
	}
}

func TestValidateSolQuestionFieldConstraints(t *testing.T) {
	base := func(question string) Result {
		review := passResult()
		review.Status = StatusNeedsSolReview
		review.Risk = RiskHigh
		review.SolQuestion = question
		return review
	}
	if err := ValidateReviewerResult(base(strings.Repeat("q", MaxFieldBytes))); err != nil {
		t.Fatalf("MaxFieldBytesちょうどのsol_questionは受理されるべき: %v", err)
	}
	cases := []struct {
		name     string
		question string
		want     string
	}{
		{"newline", "line1\nline2", "field sol_questionに改行"},
		{"carriage return", "line1\rline2", "field sol_questionに改行"},
		{"oversize", strings.Repeat("q", MaxFieldBytes+1), "field sol_questionは1536 bytes以内"},
		{"empty", "", "必須field sol_question"},
		{"whitespace only", " \t ", "必須field sol_question"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateReviewerResult(base(c.question))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !IsConstraintError(err) {
				t.Fatalf("error must be constraint, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

var textFieldSetters = map[string]func(*Result, string){
	"summary":              func(r *Result, v string) { r.Summary = v },
	"requirement_coverage": func(r *Result, v string) { r.RequirementCoverage = v },
	"tests":                func(r *Result, v string) { r.Tests = v },
	"unverified":           func(r *Result, v string) { r.Unverified = v },
	"decision":             func(r *Result, v string) { r.Decision = v },
	"evidence":             func(r *Result, v string) { r.Evidence = v },
	"options":              func(r *Result, v string) { r.Options = v },
	"recommendation":       func(r *Result, v string) { r.Recommendation = v },
	"test_obligations":     func(r *Result, v string) { r.TestObligations = v },
	"invariants":           func(r *Result, v string) { r.Invariants = v },
	"test_evidence":        func(r *Result, v string) { r.TestEvidence = v },
	"issues":               func(r *Result, v string) { r.Issues = v },
	"residual_risk":        func(r *Result, v string) { r.ResidualRisk = v },
	"sol_question":         func(r *Result, v string) { r.SolQuestion = v },
}

func fullyPopulatedResult(status Status) Result {
	result := Result{
		Status:              status,
		Summary:             "summary-value",
		RequirementCoverage: "coverage-value",
		Tests:               "tests-value",
		Unverified:          "unverified-value",
		Decision:            "decision-value",
		Evidence:            "evidence-value",
		Options:             "options-value",
		Recommendation:      "recommendation-value",
		TestObligations:     "obligations-value",
		Invariants:          "invariants-value",
		TestEvidence:        "test-evidence-value",
		Issues:              "issues-value",
		ResidualRisk:        "residual-value",
		SolQuestion:         "question-value",
		Targets:             []string{"a.go:f"},
		Artifacts:           []string{"/tmp/x"},
	}
	switch status {
	case StatusNeedsSolDecision, StatusNeedsSolReview:
		result.Risk = RiskHigh
	default:
		result.Risk = RiskLow
	}
	return result
}

func TestContractFieldsSingleSource(t *testing.T) {
	cases := map[Status]func(Result) error{
		StatusImplemented:      ValidateWorkerResult,
		StatusNeedsSolDecision: ValidateWorkerResult,
		StatusPass:             ValidateReviewerResult,
		StatusFixRequired:      ValidateReviewerResult,
		StatusNeedsSolReview:   ValidateReviewerResult,
	}
	for status, validate := range cases {
		t.Run(string(status), func(t *testing.T) {
			result := fullyPopulatedResult(status)
			if err := validate(result); err != nil {
				t.Fatalf("全契約fieldを満たす正例が拒否されました: %v", err)
			}
			contract := result.contractFields()
			contractKeys := make(map[string]bool, len(contract))
			for _, field := range contract {
				contractKeys[field.machine] = true
				setter, ok := textFieldSetters[field.machine]
				if !ok {
					t.Fatalf("contractFieldsにtextFieldSetters未対応のfieldがあります: %s", field.machine)
				}
				blanked := fullyPopulatedResult(status)
				setter(&blanked, " ")
				err := validate(blanked)
				if err == nil || !IsConstraintError(err) || !strings.Contains(err.Error(), "必須field "+field.machine) {
					t.Fatalf("%sを空にした場合の必須field errorが出ていません: %v", field.machine, err)
				}
			}
			for machine, setter := range textFieldSetters {
				if contractKeys[machine] {
					continue
				}
				noisy := fullyPopulatedResult(status)
				setter(&noisy, "noise("+string(status)+")\n"+strings.Repeat("x", MaxFieldBytes))
				if err := validate(noisy); err != nil {
					t.Fatalf("契約外field %sがvalidation対象になっています: %v", machine, err)
				}
				data, err := noisy.MachineJSON()
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if strings.Contains(string(data), `"`+machine+`"`) {
					t.Fatalf("契約外field %sがmachine JSONへ流出: %s", machine, data)
				}
			}
			data, err := result.MachineJSON()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			var object map[string]any
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatalf("machine JSON parse: %v", err)
			}
			wantKeys := map[string]bool{"status": true, "risk": true, "targets": true, "artifacts": true}
			for _, field := range contract {
				wantKeys[field.machine] = true
			}
			gotKeys := make(map[string]bool, len(object))
			for key := range object {
				gotKeys[key] = true
			}
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Fatalf("machine JSON key集合がcontractFieldsと一致しません: got %v want %v", gotKeys, wantKeys)
			}
		})
	}
}

func TestMachineJSONRoundTrip(t *testing.T) {
	for name, result := range map[string]Result{
		"implemented": implementedResult(),
		"decision": {
			Status:          StatusNeedsSolDecision,
			Risk:            RiskHigh,
			Decision:        "d",
			Evidence:        "e",
			Options:         "o",
			Recommendation:  "r",
			TestObligations: "t",
			Targets:         []string{"none"},
		},
		"pass":   passResult(),
		"review": {Status: StatusNeedsSolReview, Risk: RiskHigh, Summary: "s", RequirementCoverage: "c", Invariants: "i", TestEvidence: "e", Issues: "n", ResidualRisk: "r", Targets: []string{"a.go"}, SolQuestion: "q"},
	} {
		data, err := result.MachineJSON()
		if err != nil {
			t.Fatalf("%s: err = %v", name, err)
		}
		parsed, err := ParseStructured(data)
		if err != nil {
			t.Fatalf("%s: parse err = %v", name, err)
		}
		original, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("%s: marshal err = %v", name, err)
		}
		reparsed, err := ParseStructured(original)
		if err != nil {
			t.Fatalf("%s: baseline parse err = %v", name, err)
		}
		if !reflect.DeepEqual(parsed, reparsed) {
			t.Fatalf("%s: roundtrip mismatch:\nmachine=%s\nstruct=%s", name, data, original)
		}
	}
}

func TestIsReportOnlyFix(t *testing.T) {
	fix := Result{Status: StatusFixRequired, Targets: []string{ReportOnlyTargets}}
	if !IsReportOnlyFix(fix) {
		t.Fatal("reserved targets must be report-only")
	}
	fix.Targets = []string{ReportOnlyTargets, "other"}
	if IsReportOnlyFix(fix) {
		t.Fatal("mixed targets must not be report-only")
	}
	normal := Result{Status: StatusFixRequired, Targets: []string{"a.go"}}
	if IsReportOnlyFix(normal) {
		t.Fatal("normal targets must not be report-only")
	}
	pass := Result{Status: StatusPass, Targets: []string{ReportOnlyTargets}}
	if IsReportOnlyFix(pass) {
		t.Fatal("PASS must not be report-only")
	}
}

func TestRejectCategory(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&mismatchError{reason: "structured_outputをResultへ解析できません"}, "schema-mismatch"},
		{&constraintError{reason: "結果に必須field SUMMARYがありません"}, "missing-field"},
		{&constraintError{reason: "NEEDS_SOL_REVIEWのTARGETSはnoneにできません"}, "targets-none"},
		{&constraintError{reason: "FIX_REQUIREDのTARGETSは空にできません"}, "targets-none"},
		{&constraintError{reason: "NEEDS_SOL_DECISIONのTARGETSは空にできません"}, "targets-none"},
		{&constraintError{reason: "PASSのriskはLOWにしてください"}, "risk"},
		{&constraintError{reason: "reviewer結果のstatusとして許容されません"}, "status"},
		{&constraintError{reason: "ARTIFACTSのパスが重複しています"}, "artifacts"},
		{&constraintError{reason: "field summaryに改行を含められません"}, "multiline-field"},
		{&constraintError{reason: "field summaryは1536 bytes以内にしてください"}, "size"},
		{&constraintError{reason: "other"}, "other"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := RejectCategory(c.err); got != c.want {
			t.Fatalf("category = %q, want %q (%v)", got, c.want, c.err)
		}
	}
}
