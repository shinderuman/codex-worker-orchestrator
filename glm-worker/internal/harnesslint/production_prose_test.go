package harnesslint

import "testing"

func TestProductionProseDataRejectsExplanationConstants(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", `package x
const firstBasis = "this explanatory natural language sentence restates how the first runtime field was derived and is not a machine code"
const secondNote = "this explanatory natural language sentence restates how the second runtime field was derived and is not a machine code"
const thirdRule = "this explanatory natural language sentence restates how the third runtime field was derived and is not a machine code"
`)
	requireRulePath(t, ruleViolations(t, root), "production-prose-data", "glm-worker/internal/x/x.go")
}

func TestProductionProseDataRejectsEquivalentContainers(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", `package x
var analysisNotes = []string{
	"this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	"this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	"this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
}
var analysisRules = map[string]string{
	"basis": "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	"note": "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	"rule": "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
}
var analysisMetadata = struct { Basis, Note, Rule string }{
	Basis: "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	Note: "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
	Rule: "this explanatory natural language sentence restates runtime derivation behavior instead of using a bounded code or structural state",
}
`)
	requireRulePath(t, ruleViolations(t, root), "production-prose-data", "glm-worker/internal/x/x.go")
}

func TestProductionProseDataRejectsExplanationHelperReturns(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", `package x
func primaryBasis() string {
	return "this explanatory natural language sentence restates runtime derivation behavior instead of returning a bounded machine code"
}
func fallbackNote() string {
	return "this explanatory natural language sentence restates runtime derivation behavior instead of returning a bounded machine code"
}
func selectionRule() string {
	return "this explanatory natural language sentence restates runtime derivation behavior instead of returning a bounded machine code"
}
`)
	requireRulePath(t, ruleViolations(t, root), "production-prose-data", "glm-worker/internal/x/x.go")
}

func TestProductionProseDataAllowsSingleLiteralAndPromptPayload(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", `package x
const diagnostic = "this single authoritative diagnostic sentence is allowed because the guard targets proliferation rather than banning natural language literals"
func retryPrompt() string {
	return "this model prompt is intentionally natural language because the text itself is the runtime payload sent to the model and is not explanation metadata"
}
`)
	for _, violation := range ruleViolations(t, root) {
		if violation.Rule == "production-prose-data" {
			t.Fatalf("legitimate prose role rejected: %+v", violation)
		}
	}
}
