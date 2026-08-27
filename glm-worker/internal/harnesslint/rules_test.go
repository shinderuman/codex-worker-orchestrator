package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntrypointThin(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/cmd/good/main.go", "package main\nfunc main() { run() }\n")
	writeFixture(t, root, "glm-worker/cmd/bad/main.go", "package main\nfunc helper() {}\nfunc main() { helper() }\n")
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "entrypoint-thin", "glm-worker/cmd/bad/main.go")
}

func TestProseContractPin(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x_test.go", `package x
func TestX() {
	data := "codex/instructions/rule.md"
	_ = data
	_ = strings.Contains(data, "この長い自然言語の文章そのものをテストから固定すると第二の機械契約になってしまうため禁止する")
}
`)
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "prose-contract-pin", "glm-worker/internal/x/x_test.go")
}

func TestInstructionContentHash(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x_test.go", `package x
import "crypto/sha256"
func TestX() {
	_ = sha256.Sum256([]byte("codex/instructions/rule.md"))
}
`)
	writeFixture(t, root, "glm-worker/scenarios/x-manifest.json", `{"auto_resume_instruction_sha256":"abc"}`)
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "instruction-content-hash", "glm-worker/internal/x/x_test.go")
	requireRulePath(t, violations, "instruction-content-hash", "glm-worker/scenarios/x-manifest.json")
}

func TestShadowProductionAndScenarioSelfTest(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/scenario_test.go", `package x
func orchestrate(v int) int {
	if v > 0 {
		if v > 1 {
			for v > 2 {
				v--
				if v == 4 {
					return v
				}
			}
		}
	}
	switch v {
	case 0:
		return 1
	case 1:
		return 2
	default:
		return 3
	}
}
func TestCorpusContract() {
	_ = "glm-worker/scenarios/x.json"
	manifest := struct{ ScenarioCount int }{1}
	_ = manifest.ScenarioCount
}
`)
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "test-shadow-production", "glm-worker/internal/x/scenario_test.go")
	requireRulePath(t, violations, "scenario-self-test", "glm-worker/internal/x/scenario_test.go")
}

func TestShadowProductionDoesNotRejectOrdinaryComplexTestHelper(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/process_test.go", `package x
func processFixture(v int) int {
	if v > 0 { v-- }
	if v > 1 { v-- }
	if v > 2 { v-- }
	if v > 3 { v-- }
	return v
}
`)
	for _, violation := range ruleViolations(t, root) {
		if violation.Rule == "test-shadow-production" {
			t.Fatalf("ordinary fixture helper must not be treated as shadow production: %+v", violation)
		}
	}
}

func TestTestSizeLimit(t *testing.T) {
	root := fixtureRoot(t)
	var body strings.Builder
	body.WriteString("package x\nimport \"testing\"\nfunc TestHuge(t *testing.T) {\n")
	for i := 0; i < 151; i++ {
		body.WriteString("t.Log(\"x\")\n")
	}
	body.WriteString("}\n")
	writeFixture(t, root, "glm-worker/internal/x/x_test.go", body.String())
	requireRulePath(t, ruleViolations(t, root), "test-size-limit", "glm-worker/internal/x/x_test.go")
}

func TestModuleBoundaryAndThinWrapper(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "tools/x/go.mod", "module example.com/x\n")
	writeFixture(t, root, "glm-worker/internal/x/x.go", "package x\nfunc inner(v int) int { return v }\nfunc wrapper(v int) int { return inner(v) }\n")
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "module-boundary", "tools/x/go.mod")
	requireRulePath(t, violations, "thin-wrapper-proliferation", "glm-worker/internal/x/x.go")
}

func TestSmokeScopeAndQualityBypass(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "tests/install_smoke.sh", "#!/bin/sh\nmake_go_shim\nexpect_go_test_contract\nANTHROPIC_AUTH_TOKEN=x\ngo test ./... || true\n")
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "smoke-test-scope", "tests/install_smoke.sh")
	requireRulePath(t, violations, "quality-bypass", "tests/install_smoke.sh")
}

func TestMarkdownBudgetRouting(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "codex/AGENTS.md", "# routes\n")
	writeFixture(t, root, "codex/instructions/new-rule.md", "# rule\n")
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "markdown-runtime-budget", "codex/AGENTS.md")
}

func TestQualityConfigPolicy(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, ".golangci.yml", `version: "2"
linters:
  default: none
  enable:
    - bodyclose
`)
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "quality-config-policy", ".golangci.yml")
}

func TestStaleAuthority(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", "package x\nvar x = \"EVAL.md\"\n")
	writeFixture(t, root, "IMPLEMENTATION_HISTORY.md", "EVAL.md historical\n")
	violations := ruleViolations(t, root)
	requireRulePath(t, violations, "stale-authority-reference", "glm-worker/internal/x/x.go")
	for _, violation := range violations {
		if violation.Rule == "stale-authority-reference" && violation.Path == "IMPLEMENTATION_HISTORY.md" {
			t.Fatalf("history must be excluded: %+v", violation)
		}
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "glm-worker/go.mod", "module example.com/root\n")
	return root
}

func ruleViolations(t *testing.T, root string) []Violation {
	t.Helper()
	paths, err := repositoryPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	violations, err := checkRules(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	return violations
}

func requireRulePath(t *testing.T, violations []Violation, rule, path string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Rule == rule && violation.Path == path {
			return
		}
	}
	var lines []string
	for _, violation := range violations {
		lines = append(lines, violation.Rule+":"+violation.Path)
	}
	t.Fatalf("missing %s:%s in %s", rule, path, strings.Join(lines, ", "))
}

func writeFixture(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
