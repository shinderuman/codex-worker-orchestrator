package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyEntrypointRuleRejectsInertCoverageButPreservesBehaviorChecks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		source        string
		wantViolation bool
	}{
		{
			name:          "empty test",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) {}\n",
			wantViolation: true,
		},
		{
			name:          "comment only is empty",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { /* validates the contract */ }\n",
			wantViolation: true,
		},
		{
			name:          "parallel-only entrypoint is inert",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Parallel() }\n",
			wantViolation: true,
		},
		{
			name:          "helper and parallel without assertions are inert",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Helper(); t.Parallel() }\n",
			wantViolation: true,
		},
		{
			name:          "unconditional skip has no coverage",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Skip(\"disabled\") }\n",
			wantViolation: true,
		},
		{
			name:          "parallel then unconditional skip has no coverage",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Parallel(); t.SkipNow() }\n",
			wantViolation: true,
		},
		{
			name:          "static log only does not check behavior",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Log(\"verified\") }\n",
			wantViolation: true,
		},
		{
			name:          "static formatted log only is inert",
			source:        "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Helper(); t.Logf(\"verified\") }\n",
			wantViolation: true,
		},
		{
			name:   "log can evaluate runtime behavior",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Log(observeRuntime()) }\n",
		},
		{
			name:   "formatted log can evaluate runtime behavior",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Logf(\"value %v\", observeRuntime()) }\n",
		},
		{
			name:   "conditional platform skip preserves actual assertions",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { if special { t.Skip(\"unsupported\") }; if wrong { t.Fatal(\"bad\") } }\n",
		},
		{
			name:   "parallel before real verification is permitted",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { t.Parallel(); if 2+2 != 4 { t.Fatal(\"wrong\") } }\n",
		},
		{
			name:   "behavioral negative case",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { if 2 + 2 != 4 { t.Fatal(\"unexpected result\") } }\n",
		},
		{
			name:   "schema guard with explicit assertion",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { got := \"schema\"; if got == \"\" { t.Fatal(\"empty schema\") } }\n",
		},
		{
			name:   "table driven regression",
			source: "package demo\nimport \"testing\"\nfunc TestGuard(t *testing.T) { for _, value := range []int{1, 2} { if value < 1 { t.Fatal(value) } } }\n",
		},
		{
			name:   "ordinary non-test function",
			source: "package demo\nfunc Helper() {}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rel := "internal/demo/demo_test.go"
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			violations, err := scanGoRules(root, []string{rel})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, violation := range violations {
				if violation.Rule == "empty-test-entrypoint" {
					found = true
					if violation.Path != rel || violation.Line <= 0 {
						t.Fatalf("violation location: %#v", violation)
					}
				}
			}
			if found != tc.wantViolation {
				t.Fatalf("empty test violation=%v want=%v, all=%#v", found, tc.wantViolation, violations)
			}
		})
	}
}
