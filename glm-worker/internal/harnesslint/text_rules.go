package harnesslint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var quotedLongText = regexp.MustCompile(`'([^']{32,})'|"([^"]{32,})"`)

var markdownBudgets = map[string]int{
	"codex/AGENTS.md":                      8000,
	"codex/instructions/glm-execution.md":  19000,
	"codex/glm-worker/prompts/WORKER.md":   10000,
	"codex/glm-worker/prompts/REVIEWER.md": 10000,
	"README.md":                            14000,
}

func scanTextRules(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range paths {
		if strings.HasSuffix(path, ".json") {
			data, err := readRegularFile(root, path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, instructionHashJSONViolations(path, data)...)
		}
		if isShellPath(path) {
			data, err := readRegularFile(root, path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, prosePinShellViolations(path, data)...)
			violations = append(violations, qualityBypassViolations(path, data)...)
			if path == "tests/install_smoke.sh" {
				violations = append(violations, smokeScopeViolations(path, data)...)
			}
		}
	}
	violations = append(violations, moduleBoundaryViolations(paths)...)
	markdown, err := markdownBudgetViolations(root, paths)
	if err != nil {
		return nil, err
	}
	violations = append(violations, markdown...)
	stale, err := staleAuthorityViolations(root, paths)
	if err != nil {
		return nil, err
	}
	violations = append(violations, stale...)
	config, err := qualityConfigViolations(root, paths)
	if err != nil {
		return nil, err
	}
	violations = append(violations, config...)
	return violations, nil
}

func qualityConfigViolations(root string, paths []string) ([]Violation, error) {
	const path = ".golangci.yml"
	if !containsString(paths, path) {
		return []Violation{{Rule: "quality-config-policy", Path: path, Line: 1, Column: 1, Message: "canonical golangci-lint v2 configuration is missing"}}, nil
	}
	data, err := readRegularFile(root, path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	required := []string{`version: "2"`, "    - bodyclose", "    - cyclop", "    - decorder", "    - dupl", "    - errcheck", "    - errorlint", "    - funlen", "    - gocognit", "    - goconst", "    - gocritic", "    - govet", "    - ineffassign", "    - interfacebloat", "    - nolintlint", "    - revive", "    - sqlclosecheck", "    - staticcheck", "    - unused", "    - forbidigo", "      disable-dec-order-check: false", "      require-explanation: true", "      require-specific: true"}
	var violations []Violation
	for _, token := range required {
		if strings.Contains(text, token) {
			continue
		}
		violations = append(violations, Violation{Rule: "quality-config-policy", Path: path, Line: 1, Column: 1, Message: "required quality configuration is missing: " + strings.TrimSpace(token)})
	}
	for _, token := range []string{"exclusions:", "exclude-rules:", "skip-dirs:", "skip-files:"} {
		if line := lineOf(text, token); line > 0 {
			violations = append(violations, Violation{Rule: "quality-config-policy", Path: path, Line: line, Column: 1, Message: "quality exclusions and skip lists are forbidden"})
		}
	}
	limits := []struct {
		key string
		max int
	}{{"max-complexity", 12}, {"min-complexity", 15}, {"threshold", 100}, {"lines", 60}, {"statements", 40}, {"min-len", 3}, {"min-occurrences", 3}, {"max", 5}}
	for _, limit := range limits {
		values := yamlIntegerValues(text, limit.key)
		if len(values) == 0 {
			violations = append(violations, Violation{Rule: "quality-config-policy", Path: path, Line: 1, Column: 1, Message: "protected quality threshold is missing: " + limit.key})
			continue
		}
		for _, value := range values {
			if value > limit.max {
				violations = append(violations, Violation{Rule: "quality-config-policy", Path: path, Line: lineOf(text, limit.key+":"), Column: 1, Message: fmt.Sprintf("%s=%d weakens protected maximum %d", limit.key, value, limit.max)})
			}
		}
	}
	return violations, nil
}

func yamlIntegerValues(text, key string) []int {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*(\d+)\s*$`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	values := make([]int, 0, len(matches))
	for _, match := range matches {
		value, err := strconv.Atoi(match[1])
		if err == nil {
			values = append(values, value)
		}
	}
	return values
}

func instructionHashJSONViolations(path string, data []byte) []Violation {
	if !strings.Contains(path, "/scenarios/") && !strings.HasPrefix(path, "glm-worker/scenarios/") {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil || !containsInstructionHashKey(value) {
		return nil
	}
	return []Violation{{Rule: "instruction-content-hash", Path: path, Line: 1, Column: 1, Message: "scenario metadata must not pin whole instruction or prompt file hashes"}}
}

func containsInstructionHashKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "sha256") && (strings.Contains(lower, "instruction") || strings.Contains(lower, "prompt")) {
				return true
			}
			if containsInstructionHashKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsInstructionHashKey(child) {
				return true
			}
		}
	}
	return false
}

func prosePinShellViolations(path string, data []byte) []Violation {
	text := string(data)
	if !hasDocumentReference(text) {
		return nil
	}
	var violations []Violation
	for index, raw := range bytes.Split(data, []byte("\n")) {
		line := string(raw)
		if !strings.Contains(line, "grep") || !strings.Contains(line, "-F") {
			continue
		}
		for _, match := range quotedLongText.FindAllStringSubmatch(line, -1) {
			value := match[1]
			if value == "" {
				value = match[2]
			}
			if proseLike(value) {
				violations = append(violations, Violation{Rule: "prose-contract-pin", Path: path, Line: index + 1, Column: 1, Message: "shell test must not grep long natural-language instruction or Markdown prose"})
			}
		}
	}
	return violations
}

func smokeScopeViolations(path string, data []byte) []Violation {
	text := string(data)
	patterns := []struct{ needle, message string }{{"make_go_shim", "install smoke must not replace the Go toolchain with a call-order shim"}, {"expect_go_test_contract", "install smoke must not assert internal go test invocation counts or ordering"}, {"invocations.log", "install smoke must not maintain an internal test-runner invocation ledger"}, {"ANTHROPIC_AUTH_TOKEN", "install smoke must stay offline and must not require provider credentials"}, {"ANTHROPIC_BASE_URL", "install smoke must stay offline and must not require provider endpoints"}}
	var violations []Violation
	for _, pattern := range patterns {
		if line := lineOf(text, pattern.needle); line > 0 {
			violations = append(violations, Violation{Rule: "smoke-test-scope", Path: path, Line: line, Column: 1, Message: pattern.message})
		}
	}
	return violations
}

func qualityBypassViolations(path string, data []byte) []Violation {
	var violations []Violation
	for index, raw := range bytes.Split(data, []byte("\n")) {
		line := string(raw)
		if !strings.Contains(line, "|| true") || strings.Contains(line, "command -v") {
			continue
		}
		lower := strings.ToLower(line)
		if containsAny(lower, "lint", "test", "quality", "gate", "vet", "shellcheck", "shfmt", "golangci") {
			violations = append(violations, Violation{Rule: "quality-bypass", Path: path, Line: index + 1, Column: 1, Message: "quality command failure must not be converted to success with || true"})
		}
	}
	return violations
}

func moduleBoundaryViolations(paths []string) []Violation {
	var violations []Violation
	for _, path := range paths {
		if filepath.Base(path) != "go.mod" || path == "glm-worker/go.mod" {
			continue
		}
		violations = append(violations, Violation{Rule: "module-boundary", Path: path, Line: 1, Column: 1, Message: "glm-worker/go.mod is the canonical Go module; additional modules require explicit architecture approval"})
	}
	return violations
}

func markdownBudgetViolations(root string, paths []string) ([]Violation, error) {
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[path] = true
	}
	var violations []Violation
	for path, limit := range markdownBudgets {
		if !present[path] {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		if len(data) > limit {
			violations = append(violations, Violation{Rule: "markdown-runtime-budget", Path: path, Line: 1, Column: 1, Message: fmt.Sprintf("runtime Markdown is %d bytes over budget %d", len(data)-limit, limit)})
		}
	}
	if !present["codex/AGENTS.md"] {
		return violations, nil
	}
	agents, err := readRegularFile(root, "codex/AGENTS.md")
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if filepath.Dir(path) == "codex/instructions" && strings.HasSuffix(path, ".md") && !bytes.Contains(agents, []byte(filepath.Base(path))) {
			violations = append(violations, Violation{Rule: "markdown-runtime-budget", Path: "codex/AGENTS.md", Line: 1, Column: 1, Message: "routing lacks codex/instructions/" + filepath.Base(path)})
		}
	}
	return violations, nil
}

func staleAuthorityViolations(root string, paths []string) ([]Violation, error) {
	staleFiles := []string{"EVAL.md", "tests/install-smoke-coverage.md", "codex/instructions/install-smoke-evidence.md"}
	var violations []Violation
	for _, path := range paths {
		if isParentMetadata(path) || strings.HasPrefix(path, "glm-worker/internal/harnesslint/") {
			continue
		}
		if containsString(staleFiles, path) || staleScenarioManifest(path) {
			violations = append(violations, Violation{Rule: "stale-authority-reference", Path: path, Line: 1, Column: 1, Message: "obsolete authority or test-framework artifact must be removed"})
			continue
		}
		if !isTextPath(path) {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		for _, stale := range staleFiles {
			if bytes.Contains(data, []byte(stale)) {
				violations = append(violations, Violation{Rule: "stale-authority-reference", Path: path, Line: lineOf(string(data), stale), Column: 1, Message: "live code or instruction references obsolete authority " + stale})
			}
		}
	}
	return violations, nil
}

func staleScenarioManifest(path string) bool {
	if !strings.HasPrefix(path, "glm-worker/scenarios/") || !strings.HasSuffix(path, ".json") {
		return false
	}
	base := filepath.Base(path)
	return base == "manifest.json" || strings.HasSuffix(base, "-manifest.json")
}

func isParentMetadata(path string) bool {
	return path == "IMPLEMENTATION_RULES.md" || path == "IMPLEMENTATION_PLAN.local.md" || path == "IMPLEMENTATION_HISTORY.md" || strings.HasPrefix(path, "IMPLEMENTATION_TASKS/")
}

func isTextPath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".go", ".sh", ".md", ".json", ".toml", ".rules", ".txt", ".yml", ".yaml":
		return true
	default:
		return filepath.Base(path) == "commentlint" || filepath.Base(path) == "harnesslint" || path == ".githooks/post-merge"
	}
}

func isShellPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(path, ".sh") || base == "commentlint" || base == "harnesslint" || path == ".githooks/post-merge"
}

func lineOf(text, needle string) int {
	offset := strings.Index(text, needle)
	if offset < 0 {
		return 0
	}
	return strings.Count(text[:offset], "\n") + 1
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
