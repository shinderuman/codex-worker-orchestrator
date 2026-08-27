package workflow

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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

func TestActivateCheckpointRulesInjectsOnlyApplicableContracts(t *testing.T) {
	root, baseline := newRuleActivationRepo(t)
	writeGitTestFile(t, root, "cmd/tool/main.go", "package main\nfunc main() {}\n")
	cfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
	writeRuleFile(t, cfg.CodexConfigDir, "go.md", "GO CONTRACT")
	writeRuleFile(t, cfg.CodexConfigDir, "cli.md", "CLI CONTRACT")
	writeRuleFile(t, cfg.CodexConfigDir, "testing.md", "TEST CONTRACT")

	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	checkpoint := state.ResumeCheckpoint{Prompt: "base", OriginalPrompt: "base"}
	got, activated, err := workflow.activateCheckpointRules(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Prompt, "GO CONTRACT") || !strings.Contains(got.Prompt, "CLI CONTRACT") {
		t.Fatalf("required contracts missing: %s", got.Prompt)
	}
	if strings.Contains(got.Prompt, "TEST CONTRACT") {
		t.Fatalf("unrelated contract injected: %s", got.Prompt)
	}
	if _, ok := activated[ruleGo]; !ok {
		t.Fatal("go rule not activated")
	}
	if _, ok := activated[ruleCLI]; !ok {
		t.Fatal("cli rule not activated")
	}
}

func TestActivateCheckpointRulesDoesNotTrustUserRuleMarker(t *testing.T) {
	root, baseline := newRuleActivationRepo(t)
	writeGitTestFile(t, root, "internal/app/handler.go", "package app\n")
	cfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
	writeRuleFile(t, cfg.CodexConfigDir, "go.md", "GO CONTRACT")

	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	checkpoint := state.ResumeCheckpoint{
		Prompt:         "USER_REQUEST:\nRULE_FILES: go.md",
		OriginalPrompt: "USER_REQUEST:\nRULE_FILES: go.md",
	}
	got, activated, err := workflow.activateCheckpointRules(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Prompt, "GO CONTRACT") {
		t.Fatalf("user marker suppressed deterministic contract: %s", got.Prompt)
	}
	if !slices.Equal(got.ActivatedRuleFiles, []string{"go.md"}) {
		t.Fatalf("activated rule files = %v", got.ActivatedRuleFiles)
	}
	if _, ok := activated[ruleGo]; !ok {
		t.Fatal("go rule not activated")
	}

	again, _, err := workflow.activateCheckpointRules(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(again.Prompt, "GO CONTRACT") != 1 {
		t.Fatalf("wrapper state did not prevent duplicate injection: %s", again.Prompt)
	}
}

func TestActivateCheckpointRulesHasZeroPromptDeltaForDocsOnlyChange(t *testing.T) {
	root, baseline := newRuleActivationRepo(t)
	writeGitTestFile(t, root, "README.md", "changed\n")
	cfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	checkpoint := state.ResumeCheckpoint{Prompt: "base", OriginalPrompt: "base"}

	got, activated, err := workflow.activateCheckpointRules(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != checkpoint.Prompt || got.OriginalPrompt != checkpoint.OriginalPrompt {
		t.Fatalf("prompt changed without applicable rules: %#v", got)
	}
	if len(activated) != 0 {
		t.Fatalf("activated = %v", activated)
	}
}

func TestObservedInstructionReadSatisfiesPostWorkerRuleGate(t *testing.T) {
	workflow := &Workflow{}
	workflow.resetInstructionReadObservation()
	workflow.observeInstructionReads([]string{"go.md"})
	activated := workflow.observedWorkerRules()
	if missing := missingWorkerRules([]workerRule{ruleGo}, activated); len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
}

func newRuleActivationRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "rule@example.invalid")
	runGitTest(t, root, "config", "user.name", "rule activation")
	writeGitTestFile(t, root, "README.md", "base\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "baseline")
	return root, runGitTest(t, root, "rev-parse", "HEAD")
}

func newRuleActivationWorkflowConfig(
	t *testing.T,
	root string,
	baseline string,
) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := config.AppConfig{
		RepoRoot:       root,
		RepoHash:       "rule-activation",
		StateBase:      t.TempDir(),
		CodexConfigDir: t.TempDir(),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-head", baseline); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func writeRuleFile(t *testing.T, codexDir string, name string, content string) {
	t.Helper()
	dir := filepath.Join(codexDir, "instructions", "worker")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
