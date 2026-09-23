package harnesslint

import "testing"

func TestForwardOnlySemanticRejectsDualParentWaitTransport(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport.go"
	writeFixture(t, root, path, `package example

type item struct { Type, Name, Input string }
func observe(v item) bool {
	switch v.Type {
	case "custom_tool_call":
		return v.Name == "exec" && v.Input == "tools.write_stdin"
	case "function_call":
		return v.Name == "wait"
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

type item struct { Type, Name string }
func consume(v item) item {
	if v.Type == "custom_tool_call" {
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

func TestForwardOnlySemanticRejectsMixedTransportAcceptanceTest(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/transport_test.go"
	writeFixture(t, root, path, `package example

import "testing"
func TestBothRepresentations(t *testing.T) {
	_ = item{Type: "custom_tool_call", Name: "exec"}
	_ = item{Type: "function_call", Name: "wait"}
	_ = "tools.write_stdin"
}
`)
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example
type item struct { Type, Name string }
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlySemanticAllowsGenericToolActivityAndCurrentWait(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/transport.go", `package example

type item struct { Type, Name, Input string }
func observeTool(v item) bool {
	switch v.Type {
	case "function_call", "custom_tool_call":
		return true
	}
	return false
}
func observeWait(v item) bool {
	return v.Type == "custom_tool_call" && v.Name == "exec" && v.Input == "tools.write_stdin"
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

func TestForwardOnlySemanticRejectsNeutralHookStatePromotion(t *testing.T) {
	root := fixtureRoot(t)
	path := "scripts/install.sh"
	writeFixture(t, root, path, `#!/bin/sh
old='version=1 baseline=absent value=.githooks'
current='version=2 baseline=absent value=/managed/hooks'
case "$stored" in
"$old") state_kind=prior ;;
esac
case "$state_kind" in
prior) write_state "$current" ;;
esac
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}
