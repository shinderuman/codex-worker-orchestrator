package workflow

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

const emptyTreeObject = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type selfProtectionDecision struct {
	High    bool
	Source  string
	HitPath string
}

type pathClass struct {
	critical bool
	category string
}

var classifiedFiles = map[string]pathClass{
	"install.sh":                             {true, "installer"},
	"commentlint":                            {true, "comment-policy"},
	".githooks/post-merge":                   {true, "installer"},
	"claude/settings-managed.json":           {true, "managed-claude-settings"},
	"codex/config-managed.toml":              {true, "managed-codex-config"},
	"glm-worker/go.mod":                      {true, "dependency-manifest"},
	"tools/merge-json/go.mod":                {true, "dependency-manifest"},
	"codex/AGENTS.md":                        {true, "managed-agents"},
	"AGENTS.md":                              {true, "repo-agents"},
	"IMPLEMENTATION_RULES.md":                {true, "implementation-rules"},
	"IMPLEMENTATION_PLAN.local.md":           {true, "implementation-plan"},
	"IMPLEMENTATION_HISTORY.md":              {true, "implementation-history"},
	"glm-worker/internal/state/stats.go":     {false, "observation"},
	"glm-worker/internal/state/telemetry.go": {false, "observation"},
	"README.md":                              {false, "docs"},
	"EVAL.md":                                {false, "docs"},
	"LICENSE":                                {false, "docs"},
	".gitignore":                             {false, "repo-metadata"},
}

var internalPackageCategories = map[string]string{
	"workflow":   "workflow-package",
	"packet":     "packet-package",
	"runner":     "runner-package",
	"app":        "app-package",
	"config":     "config-package",
	"state":      "state-critical",
	"autoresume": "autoresume-package",
}

func IsCriticalPath(path string) (bool, string) {
	if path == "" {
		return false, ""
	}
	if c, ok := classifiedFiles[path]; ok {
		return c.critical, c.category
	}
	switch {
	case isProductionGoUnder(path, "glm-worker/internal/"):
		return true, internalPackageCategory(path)
	case isProductionGoUnder(path, "glm-worker/cmd/"):
		return true, "worker-entrypoint"
	case isProductionGoUnder(path, "tools/merge-json/"):
		return true, "merge-tool"
	case strings.HasPrefix(path, "glm-worker/scenarios/"):
		return true, "scenario-corpus"
	case strings.HasPrefix(path, "codex/glm-worker/prompts/"):
		return true, "managed-prompts"
	case strings.HasPrefix(path, "codex/instructions/"):
		return true, "managed-instructions"
	case strings.HasPrefix(path, "codex/rules/"):
		return true, "managed-rules"
	case strings.HasPrefix(path, "IMPLEMENTATION_TASKS/"):

		return true, "implementation-tasks"
	case strings.HasSuffix(path, "_test.go"):
		return false, "test"
	case strings.Contains(path, "testdata/"):
		return false, "test-fixture"
	case strings.HasPrefix(path, "tests/"), strings.HasPrefix(path, "glm-worker/scripts/"):
		return false, "test-harness"
	}
	return false, ""
}

func internalPackageCategory(path string) string {
	rest := strings.TrimPrefix(path, "glm-worker/internal/")
	pkg := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		pkg = rest[:i]
	}
	if cat, ok := internalPackageCategories[pkg]; ok {
		return cat
	}
	return "internal-production"
}

func isProductionGoUnder(path, dir string) bool {
	if !strings.HasPrefix(path, dir) {
		return false
	}
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	return !strings.HasSuffix(path, "_test.go")
}

func classifySelfProtection(paths []string) selfProtectionDecision {
	categories := make(map[string]struct{})
	var firstHit string
	for _, p := range paths {
		if ok, cat := IsCriticalPath(p); ok {
			categories[cat] = struct{}{}
			if firstHit == "" {
				firstHit = p
			}
		}
	}
	if len(categories) == 0 {
		return selfProtectionDecision{High: false}
	}
	cats := make([]string, 0, len(categories))
	for c := range categories {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return selfProtectionDecision{High: true, Source: strings.Join(cats, ","), HitPath: firstHit}
}

func collectChangedPaths(repoRoot, baselineHead string) ([]string, error) {
	base := baselineHead
	if strings.TrimSpace(base) == "" {
		base = emptyTreeObject
	}
	tracked, err := exec.Command("git", "-C", repoRoot, "diff", "--no-renames", "--name-only", "-z", base).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only: %w", err)
	}
	untracked, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others: %w", err)
	}
	paths := make([]string, 0, 16)
	paths = append(paths, splitNul(tracked)...)
	paths = append(paths, splitNul(untracked)...)
	return paths, nil
}

func splitNul(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	parts := bytes.Split(b, []byte{0})
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			result = append(result, string(p))
		}
	}
	return result
}
