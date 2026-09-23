package harnesslint

import "testing"

func TestForwardOnlySemanticRejectsDualParentWaitReader(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
type scan struct { waits []item }
func observe(s *scan, v item) {
	switch v.Type {
	case "function_call":
		if v.Name != "wait" { return }
		s.waits = append(s.waits, v)
	case "custom_tool_call":
		if v.Name != "exec" || v.Input != "tools.write_stdin" { return }
		s.waits = append(s.waits, v)
	}
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsDualParentWaitReaderInIfConditions(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
func observe(v item) bool {
	if v.Type == "function_call" && v.Name == "wait" {
		return true
	}
	if v.Type == "custom_tool_call" && v.Input == "tools.write_stdin" {
		return true
	}
	return false
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsCurrentToOldNormalizationAcrossNeutralHelper(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
func consume(v item) item {
	if v.Type == "custom_tool_call" { return reshape(v) }
	return v
}
func reshape(v item) item {
	if v.Input != "tools.write_stdin" { return v }
	v.Type = "function_call"
	v.Name = "wait"
	return v
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsCallerEstablishedCurrentWaitNormalization(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
func consume(v item) item {
	if v.Type == "custom_tool_call" && v.Input == "tools.write_stdin" {
		return reshape(v)
	}
	return v
}
func reshape(v item) item {
	v.Type = "function_call"
	v.Name = "wait"
	return v
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsCurrentToOldNormalizationAcrossMethod(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
type normalizer struct{}
func consume(v item) item {
	n := normalizer{}
	if v.Type == "custom_tool_call" { return n.reshape(v) }
	return v
}
func (normalizer) reshape(v item) item {
	if v.Input != "tools.write_stdin" { return v }
	v.Type = "function_call"
	v.Name = "wait"
	return v
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsMixedTransportSuccessContract(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport_test.go"
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example
type item struct { Type, Name, Input string }
type result struct { Count int }
func observe([]item) result { return result{} }
`)
	writeFixture(t, root, path, `package example
import "testing"
func TestBothRepresentationsAccepted(t *testing.T) {
	got := observe([]item{
		{Type: "function_call", Name: "wait"},
		{Type: "custom_tool_call", Name: "exec", Input: "tools.write_stdin"},
	})
	if got.Count != 2 { t.Fatal(got.Count) }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticRejectsNeutralHookStatePromotion(t *testing.T) {
	root := fixtureRoot(t)
	path := "scripts/install.sh"
	writeFixture(t, root, path, `#!/bin/sh
prior='version=1 baseline=absent value=.githooks'
active='version=2 baseline=absent value=/managed/hooks'
case "$stored" in
"$prior") state_kind=prior ;;
esac
case "$state_kind" in
prior) write_state "$active" ;;
esac
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticAllowsCurrentAcceptedRejectsLegacyInSameTest(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example
type item struct { Type, Name, Input string }
type result struct { Count int }
func observe([]item) result { return result{} }
`)
	writeFixture(t, root, "glm-worker/internal/example/transport_test.go", `package example
import "testing"
func TestCurrentAcceptedRejectsLegacy(t *testing.T) {
	got := observe([]item{
		{Type: "function_call", Name: "wait"},
		{Type: "custom_tool_call", Name: "exec", Input: "tools.write_stdin"},
	})
	if got.Count != 1 { t.Fatal(got.Count) }
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlySemanticAllowsGenericFunctionCallWithCurrentWait(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example

type item struct { Type, Name, Input string }
type scan struct { tools, waits []item }
func observe(s *scan, v item) {
	switch v.Type {
	case "function_call":
		s.tools = append(s.tools, v)
	case "custom_tool_call":
		if v.Name != "exec" || v.Input != "tools.write_stdin" { return }
		s.waits = append(s.waits, v)
	}
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlySemanticAllowsStrictArchiveEvidenceChain(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/archive.go", `package example

const currentRevision = 3
type identity struct { SchemaRevision int }
func decodeIdentity(v identity) error {
	if v.SchemaRevision != currentRevision { return errUnsupported }
	return nil
}
func ArchivedTaskStatsEvidence(v identity) error {
	if err := decodeIdentity(v); err != nil { return err }
	if v.SchemaRevision > currentRevision { return errUnsupported }
	return nil
}
var errUnsupported error
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlySemanticAllowsUnsupportedOldStateRejection(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "scripts/install.sh", `#!/bin/sh
prior='version=1 baseline=absent value=.githooks'
active='version=2 baseline=absent value=/managed/hooks'
case "$stored" in
"$active") state_kind=current ;;
"$prior") rm -f "$state_path"; exit 1 ;;
*) exit 1 ;;
esac
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlySemanticAllowsUnrelatedRewriteAssignments(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example

type item struct { Type, Name, Input string }
func consume(current, other item) item {
	if current.Type == "custom_tool_call" && current.Input == "tools.write_stdin" {
		current.Type = "function_call"
		other.Name = "wait"
	}
	return current
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}
