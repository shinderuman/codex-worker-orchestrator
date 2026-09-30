package harnesslint

import (
	"strings"
	"testing"
)

func TestDuplicateTestBodyRejectsIdenticalEntrypointsInSamePackage(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/first_test.go", `package x
import "testing"
func TestPrimary(t *testing.T) {
	got := 1
	if got != 1 {
		t.Fatal(got)
	}
}
`)
	writeFixture(t, root, "glm-worker/internal/x/second_test.go", `package x
import "testing"
func TestDuplicate(t *testing.T) {
	got := 1
	if got != 1 {
		t.Fatal(got)
	}
}
`)

	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "test-duplicate-body", "glm-worker/internal/x/second_test.go")
	for _, violation := range violations {
		if violation.Rule != "test-duplicate-body" || violation.Path != "glm-worker/internal/x/second_test.go" {
			continue
		}
		if !strings.Contains(violation.Message, "TestPrimary") || !strings.Contains(violation.Message, "first_test.go") {
			t.Fatalf("duplicate diagnostic must identify the canonical existing test: %+v", violation)
		}
		return
	}
	t.Fatal("duplicate violation not found")
}

func TestDuplicateTestBodyAllowsSemanticallyDistinctEntrypoints(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/first_test.go", `package x
import "testing"
func TestOne(t *testing.T) {
	got := 1
	if got != 1 {
		t.Fatal(got)
	}
}
`)
	writeFixture(t, root, "glm-worker/internal/x/second_test.go", `package x
import "testing"
func TestTwo(t *testing.T) {
	got := 2
	if got != 2 {
		t.Fatal(got)
	}
}
`)

	for _, violation := range ruleViolations(t, root) {
		if violation.Rule == "test-duplicate-body" {
			t.Fatalf("distinct regression cases must remain accepted: %+v", violation)
		}
	}
}

func TestDuplicateTestBodyRespectsExternalTestPackageBoundary(t *testing.T) {
	root := fixtureRoot(t)
	body := `import "testing"
func TestSameShape(t *testing.T) {
	got := 1
	if got != 1 {
		t.Fatal(got)
	}
}
`
	writeFixture(t, root, "glm-worker/internal/x/internal_test.go", "package x\n"+body)
	writeFixture(t, root, "glm-worker/internal/x/external_test.go", "package x_test\n"+body)

	for _, violation := range ruleViolations(t, root) {
		if violation.Rule == "test-duplicate-body" {
			t.Fatalf("x and x_test are distinct test owners and must not be conflated: %+v", violation)
		}
	}
}
