package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type workerRule string

const (
	ruleTesting          workerRule = "testing"
	ruleStateTransitions workerRule = "state-transitions"
	ruleCLI              workerRule = "cli"
	ruleGo               workerRule = "go"
	ruleJavaScript       workerRule = "javascript"
	rulePHP              workerRule = "php"
	ruleESLint           workerRule = "eslint"

	deterministicRuleMarker      = "DETERMINISTIC_RULE_ACTIVATION:"
	deterministicRuleFilesMarker = "RULE_FILES:"
)

var workerRuleOrder = []workerRule{
	ruleTesting,
	ruleStateTransitions,
	ruleCLI,
	ruleGo,
	ruleJavaScript,
	rulePHP,
	ruleESLint,
}

var workerRuleFiles = map[workerRule]string{
	ruleTesting:          "testing.md",
	ruleStateTransitions: "state-transitions.md",
	ruleCLI:              "cli.md",
	ruleGo:               "go.md",
	ruleJavaScript:       "javascript.md",
	rulePHP:              "php.md",
	ruleESLint:           "eslint.md",
}

var stateRuleTokens = map[string]struct{}{
	"state": {}, "states": {}, "config": {}, "configs": {}, "configuration": {},
	"setting": {}, "settings": {}, "cache": {}, "caches": {},
	"migration": {}, "migrations": {}, "upgrade": {}, "upgrades": {},
	"manifest": {}, "manifests": {}, "sidecar": {}, "sidecars": {},
	"persistent": {}, "persistence": {}, "storage": {}, "store": {},
	"database": {}, "databases": {}, "db": {},
}

var cliRuleTokens = map[string]struct{}{
	"cmd": {}, "cli": {}, "bin": {}, "command": {}, "commands": {},
	"flag": {}, "flags": {}, "arg": {}, "args": {}, "argv": {},
	"option": {}, "options": {}, "subcommand": {}, "subcommands": {},
}

func requiredWorkerRules(repoRoot string, paths []string) []workerRule {
	required := make(map[workerRule]struct{})
	javaScriptChanged := false
	for _, raw := range paths {
		path := filepath.ToSlash(raw)
		isTest := isTestingRulePath(path)
		if isTest {
			required[ruleTesting] = struct{}{}
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			required[ruleGo] = struct{}{}
		case ".js", ".mjs", ".cjs", ".jsx":
			required[ruleJavaScript] = struct{}{}
			javaScriptChanged = true
		case ".php":
			required[rulePHP] = struct{}{}
		}
		if isTest {
			continue
		}
		if pathHasRuleToken(path, stateRuleTokens) {
			required[ruleStateTransitions] = struct{}{}
		}
		if pathHasRuleToken(path, cliRuleTokens) {
			required[ruleCLI] = struct{}{}
		}
	}
	if javaScriptChanged && repositoryUsesESLint(repoRoot) {
		required[ruleESLint] = struct{}{}
	}
	return orderedWorkerRules(required)
}

func isTestingRulePath(path string) bool {
	_, category := IsCriticalPath(path)
	switch category {
	case "test", testFixturePathCategory, testHarnessPathCategory:
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(lower)
	for _, suffix := range []string{
		"_test.go", ".test.js", ".spec.js", ".test.mjs", ".spec.mjs",
		".test.cjs", ".spec.cjs", ".test.jsx", ".spec.jsx", ".test.php", ".spec.php",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	for _, segment := range strings.Split(lower, "/") {
		switch segment {
		case "test", "tests", "testdata", "__tests__", "spec", "specs":
			return true
		}
	}
	return false
}

func pathHasRuleToken(path string, tokens map[string]struct{}) bool {
	for _, segment := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		stem := strings.TrimSuffix(segment, filepath.Ext(segment))
		for _, token := range strings.FieldsFunc(stem, func(r rune) bool {
			return r == '_' || r == '-' || r == '.'
		}) {
			if _, ok := tokens[token]; ok {
				return true
			}
		}
	}
	return false
}

func repositoryUsesESLint(repoRoot string) bool {
	for _, name := range []string{
		"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs",
		".eslintrc", ".eslintrc.js", ".eslintrc.cjs", ".eslintrc.json", ".eslintrc.yml", ".eslintrc.yaml",
	} {
		if _, err := os.Stat(filepath.Join(repoRoot, name)); err == nil {
			return true
		}
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return false
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	if _, ok := manifest["eslintConfig"]; ok {
		return true
	}
	for _, section := range []string{"dependencies", "devDependencies"} {
		deps, ok := manifest[section].(map[string]any)
		if ok {
			if _, exists := deps["eslint"]; exists {
				return true
			}
		}
	}
	return false
}

func orderedWorkerRules(set map[workerRule]struct{}) []workerRule {
	result := make([]workerRule, 0, len(set))
	for _, rule := range workerRuleOrder {
		if _, ok := set[rule]; ok {
			result = append(result, rule)
		}
	}
	return result
}

func workerRuleContextBlock(codexConfigDir string, rules []workerRule) (string, error) {
	if len(rules) == 0 {
		return "", nil
	}
	var block strings.Builder
	block.WriteString("\n\n")
	block.WriteString(deterministicRuleMarker)
	block.WriteString("\n")
	block.WriteString(deterministicRuleFilesMarker)
	block.WriteString(" ")
	files := workerRuleFileNames(rules)
	block.WriteString(strings.Join(files, ","))
	block.WriteString("\n")
	block.WriteString(renderInstructionConflictBoundary(defaultInstructionConflictBoundary()))
	block.WriteString("wrapperが実diffから決定論的に選択したcontractです。以下の本文を今回の作業・reviewへ適用してください。\n")
	for _, fileName := range files {
		path := filepath.Join(codexConfigDir, "instructions", "worker", fileName)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("required worker ruleがありません: %s: %w", path, err)
		}
		block.WriteString("\n--- " + fileName + " ---\n")
		block.WriteString(strings.TrimRight(string(data), "\n"))
	}
	block.WriteString("\n")
	return block.String(), nil
}

func appendWorkerRuleContext(prompt string, codexConfigDir string, rules []workerRule) (string, error) {
	if len(rules) == 0 {
		return prompt, nil
	}
	block, err := workerRuleContextBlock(codexConfigDir, rules)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(prompt, "\n") + block, nil
}

func workerRuleFileNames(rules []workerRule) []string {
	files := make([]string, 0, len(rules))
	for _, rule := range rules {
		files = append(files, workerRuleFiles[rule])
	}
	return files
}

func workerRuleForFile(fileName string) (workerRule, bool) {
	for rule, name := range workerRuleFiles {
		if name == fileName {
			return rule, true
		}
	}
	return "", false
}
