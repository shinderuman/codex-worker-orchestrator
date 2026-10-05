package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRequiredWorkerRulesAreDerivedFromGenericPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "package.json"),
		[]byte(`{"devDependencies":{"eslint":"1.0.0"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	got := requiredWorkerRules(root, []string{
		"internal/state/store.go",
		"cmd/tool/main.go",
		"tests/case.js",
	})
	want := []workerRule{
		ruleTesting,
		ruleStateTransitions,
		ruleCLI,
		ruleGo,
		ruleJavaScript,
		ruleESLint,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
}

func TestRequiredWorkerRulesCoverPersistentAndCLIAliases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want workerRule
	}{
		{name: "settings", path: "internal/settings/loader.go", want: ruleStateTransitions},
		{name: "upgrade", path: "upgrade/schema.go", want: ruleStateTransitions},
		{name: "manifest", path: "internal/manifest/writer.go", want: ruleStateTransitions},
		{name: "sidecar", path: "internal/sidecar/file.go", want: ruleStateTransitions},
		{name: "storage", path: "internal/storage/record.go", want: ruleStateTransitions},
		{name: "database", path: "internal/database/query.go", want: ruleStateTransitions},
		{name: "flags", path: "internal/app/flags.go", want: ruleCLI},
		{name: "args", path: "internal/app/args.go", want: ruleCLI},
		{name: "options", path: "internal/app/options.go", want: ruleCLI},
		{name: "subcommand", path: "internal/app/subcommand.go", want: ruleCLI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := requiredWorkerRules(t.TempDir(), []string{tt.path})
			if !slices.Contains(rules, tt.want) {
				t.Fatalf("rules = %v, missing %s for %s", rules, tt.want, tt.path)
			}
		})
	}
}

func TestRequiredWorkerRulesDoNotRouteStateFromTestPackageName(t *testing.T) {
	got := requiredWorkerRules(t.TempDir(), []string{"internal/state/store_test.go"})
	want := []workerRule{ruleTesting, ruleGo}
	if !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
}

func TestWorkerRuleContextBlockPreservesCanonicalRendering(t *testing.T) {
	codexDir := t.TempDir()
	writeRulePolicyFile(t, codexDir, "cli.md", "CLI CONTRACT")
	writeRulePolicyFile(t, codexDir, "go.md", "GO CONTRACT")

	got, err := workerRuleContextBlock(codexDir, []workerRule{ruleCLI, ruleGo})
	if err != nil {
		t.Fatal(err)
	}
	want := "\n\n" + deterministicRuleMarker + "\n" +
		deterministicRuleFilesMarker + " cli.md,go.md\n" +
		renderInstructionConflictBoundary(defaultInstructionConflictBoundary()) +
		"wrapperが実diffから決定論的に選択したcontractです。以下の本文を今回の作業・reviewへ適用してください。\n" +
		"\n--- cli.md ---\nCLI CONTRACT" +
		"\n--- go.md ---\nGO CONTRACT\n"
	if got != want {
		t.Fatalf("context block mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func writeRulePolicyFile(t *testing.T, codexDir string, name string, content string) {
	t.Helper()
	dir := filepath.Join(codexDir, "instructions", "worker")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
