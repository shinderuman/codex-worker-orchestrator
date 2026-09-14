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

func TestForwardOnlyCompatibilityAllowsLegitimateTestFunctionValues(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/evaluator.go", `package example
func CanonicalEvaluator(value int) int { return value }
`)
	writeFixture(t, root, "glm-worker/internal/example/evaluator_test.go", `package example
import "testing"

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
