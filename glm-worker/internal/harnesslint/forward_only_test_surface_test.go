package harnesslint

import "testing"

func TestForwardOnlyCompatibilityRejectsTestOnlyCallableAliasSurface(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	aliasPath := "glm-worker/internal/example/evaluator_legacy_test.go"
	writeFixture(t, root, aliasPath, `package example
var RetiredEvaluator = CanonicalEvaluator
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"
func TestCallSurface(t *testing.T) {
	if RetiredEvaluator(1) != 1 { t.Fatal("unexpected result") }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, aliasPath)
}

func TestForwardOnlyCompatibilityRejectsAliasDespiteLocalShadows(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	aliasPath := "glm-worker/internal/example/evaluator_legacy_test.go"
	writeFixture(t, root, aliasPath, `package example
var RetiredEvaluator = CanonicalEvaluator
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_usage_test.go", `package example
import "testing"
func TestCallSurface(t *testing.T) {
	if RetiredEvaluator(1) != 1 { t.Fatal("unexpected result") }
}
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_shadow_test.go", `package example
import "testing"
func callShadow(RetiredEvaluator func(int) int) int { return RetiredEvaluator(1) }
func TestLocalShadow(t *testing.T) {
	RetiredEvaluator := func(value int) int { return value + 1 }
	if RetiredEvaluator(1) != 2 { t.Fatal("local shadow") }
	if callShadow(RetiredEvaluator) != 2 { t.Fatal("parameter shadow") }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, aliasPath)
}

func TestForwardOnlyCompatibilityAllowsLocalShadowWithoutAliasUsage(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_legacy_test.go", `package example
var RetiredEvaluator = CanonicalEvaluator
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_shadow_test.go", `package example
import "testing"
func callShadow(RetiredEvaluator func(int) int) int { return RetiredEvaluator(1) }
func TestLocalShadow(t *testing.T) {
	RetiredEvaluator := func(value int) int { return value + 1 }
	if RetiredEvaluator(1) != 2 { t.Fatal("local shadow") }
	if callShadow(RetiredEvaluator) != 2 { t.Fatal("parameter shadow") }
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlyCompatibilityRejectsExplicitlyTypedCallableAlias(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	aliasPath := "glm-worker/internal/example/evaluator_legacy_test.go"
	writeFixture(t, root, aliasPath, `package example
var RetiredEvaluator func(int) int = CanonicalEvaluator
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"
func TestCallSurface(t *testing.T) {
	if RetiredEvaluator(1) != 1 { t.Fatal("unexpected result") }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, aliasPath)
}

func TestForwardOnlyCompatibilityRejectsParenthesizedAliasSurface(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	aliasPath := "glm-worker/internal/example/evaluator_legacy_test.go"
	writeFixture(t, root, aliasPath, `package example
var RetiredEvaluator = (CanonicalEvaluator)
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"
func TestCallSurface(t *testing.T) {
	if (RetiredEvaluator)(1) != 1 { t.Fatal("unexpected result") }
}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, aliasPath)
}

func TestForwardOnlyCompatibilityAllowsReassignedPackageTestHook(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_hook_test.go", `package example
var EvaluatorHook = CanonicalEvaluator
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"
func fakeEvaluator(value int) int { return value + 1 }
func TestHook(t *testing.T) {
	saved := EvaluatorHook
	EvaluatorHook = fakeEvaluator
	defer func() { EvaluatorHook = saved }()
	if EvaluatorHook(1) != 2 { t.Fatal("dependency injection hook") }
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlyCompatibilityAllowsLegitimateTestFunctionValues(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"

var SameFileHelper = CanonicalEvaluator
var EvaluatorHook = CanonicalEvaluator
var evaluatorHook = CanonicalEvaluator
var TestCallback = CanonicalEvaluator
var StubEvaluator = fakeEvaluator

func fakeEvaluator(value int) int { return value + 1 }
func EvaluateFixture(value int) int { return CanonicalEvaluator(value) + 1 }
func useCallback(callback func(int) int) int { return callback(1) }

func TestFunctionValues(t *testing.T) {
	local := CanonicalEvaluator
	if local(1) != 1 { t.Fatal("local function value") }
	if SameFileHelper(1) != 1 { t.Fatal("same-file test helper") }

	saved := EvaluatorHook
	EvaluatorHook = fakeEvaluator
	defer func() { EvaluatorHook = saved }()
	if EvaluatorHook(1) != 2 { t.Fatal("dependency injection hook") }

	if evaluatorHook(1) != 1 { t.Fatal("package test hook") }
	if useCallback(TestCallback) != 1 { t.Fatal("test callback") }
	if StubEvaluator(1) != 2 { t.Fatal("stub") }
	if EvaluateFixture(1) != 2 { t.Fatal("independent helper") }
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}
