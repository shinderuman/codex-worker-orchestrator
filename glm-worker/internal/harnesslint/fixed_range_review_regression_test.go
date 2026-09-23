package harnesslint

import "testing"

func TestForwardOnlySemanticRejectsCombinedTransportArm(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
type scan struct { waits []item }
func observe(s *scan, v item) {
	switch v.Type {
	case "function_call", "custom_tool_call":
		if v.Name != "wait" && v.Input != "tools.write_stdin" { return }
		s.waits = append(s.waits, v)
	}
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsCurrentToOldCompositeLiteral(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
func normalize(v item) item {
	if v.Type == "custom_tool_call" && v.Input == "tools.write_stdin" {
		return item{Type: "function_call", Name: "wait"}
	}
	return v
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsMixedTransportLenFieldContract(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport_test.go"
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example

type item struct { Type, Name, Input string }
type result struct { Waits []item }
func observe([]item) result { return result{} }
`)
	writeFixture(t, root, path, `package example
import "testing"
func TestBothRepresentationsAccepted(t *testing.T) {
	got := observe([]item{
		{Type: "function_call", Name: "wait"},
		{Type: "custom_tool_call", Name: "exec", Input: "tools.write_stdin"},
	})
	if len(got.Waits) != 2 { t.Fatal("both representations must be accepted") }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}
