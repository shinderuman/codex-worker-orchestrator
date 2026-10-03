package taskcontract

import (
	"strings"
	"testing"
)

func TestParseMachineAcceptanceAbsent(t *testing.T) {
	parsed, err := ParseMachineAcceptance([]byte("# Task\n\n## Acceptance criteria\n\nsemantic only\n"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Present || len(parsed.Requirements) != 0 {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParseMachineAcceptanceStrictDocument(t *testing.T) {
	content := `# Task

## Machine-verifiable acceptance

{
  "schema": "task-machine-acceptance/v1",
  "requirements": [
    {"id": "normal-review-observed", "fact": "failure-path-advisory-observed"},
    {"id": "sol-visible-advisory", "fact": "failure-path-advisory-shown"}
  ]
}

## Dependencies
`
	parsed, err := ParseMachineAcceptance([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Present || len(parsed.Requirements) != 2 {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Requirements[0].ID != "normal-review-observed" || parsed.Requirements[0].Fact != MachineFactFailurePathAdvisoryObserved {
		t.Fatalf("first requirement = %#v", parsed.Requirements[0])
	}
	if parsed.Requirements[1].Fact != MachineFactFailurePathAdvisoryShown {
		t.Fatalf("second requirement = %#v", parsed.Requirements[1])
	}
}

func TestParseMachineAcceptanceRejectsMalformedContracts(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "", want: "JSONが空"},
		{name: "unknown field", body: `{"schema":"task-machine-acceptance/v1","requirements":[{"id":"x","fact":"failure-path-advisory-observed"}],"extra":true}`, want: "unknown field"},
		{name: "trailing value", body: `{"schema":"task-machine-acceptance/v1","requirements":[{"id":"x","fact":"failure-path-advisory-observed"}]} {}`, want: "1つだけ"},
		{name: "wrong schema", body: `{"schema":"other/v1","requirements":[{"id":"x","fact":"failure-path-advisory-observed"}]}`, want: "schemaが不正"},
		{name: "empty requirements", body: `{"schema":"task-machine-acceptance/v1","requirements":[]}`, want: "requirementsが空"},
		{name: "bad id", body: `{"schema":"task-machine-acceptance/v1","requirements":[{"id":"Bad ID","fact":"failure-path-advisory-observed"}]}`, want: "idが不正"},
		{name: "duplicate id", body: `{"schema":"task-machine-acceptance/v1","requirements":[{"id":"same","fact":"failure-path-advisory-observed"},{"id":"same","fact":"failure-path-advisory-shown"}]}`, want: "重複"},
		{name: "unsupported fact", body: `{"schema":"task-machine-acceptance/v1","requirements":[{"id":"x","fact":"some-proxy"}]}`, want: "未対応"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := "# Task\n\n## Machine-verifiable acceptance\n\n" + tc.body + "\n"
			_, err := ParseMachineAcceptance([]byte(content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v want substring %q", err, tc.want)
			}
		})
	}
}

func TestParseMachineAcceptanceRejectsDuplicateSection(t *testing.T) {
	content := `# Task

## Machine-verifiable acceptance

{"schema":"task-machine-acceptance/v1","requirements":[{"id":"one","fact":"failure-path-advisory-observed"}]}

## Machine-verifiable acceptance

{"schema":"task-machine-acceptance/v1","requirements":[{"id":"two","fact":"failure-path-advisory-observed"}]}
`
	_, err := ParseMachineAcceptance([]byte(content))
	if err == nil || !strings.Contains(err.Error(), "複数") {
		t.Fatalf("err = %v", err)
	}
}
