package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownRuntimeReadGraphBudget(t *testing.T) {
	root := scenarioRepoRoot(t)

	budgets := []struct {
		path  string
		limit int
	}{
		{"codex/AGENTS.md", 8000},
		{"codex/instructions/glm-execution.md", 19000},
		{"codex/glm-worker/prompts/WORKER.md", 10000},
		{"codex/glm-worker/prompts/REVIEWER.md", 10000},
		{"README.md", 14000},
	}
	for _, b := range budgets {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(b.path)))
		if err != nil {
			t.Fatalf("read %s: %v", b.path, err)
		}
		if len(data) > b.limit {
			t.Errorf("%s is %d bytes over the runtime read budget %d; move the event-specific contract into its own instruction file and route it from codex/AGENTS.md instead of appending here", b.path, len(data)-b.limit, b.limit)
		}
	}

	agentsBytes, err := os.ReadFile(filepath.Join(root, "codex/AGENTS.md"))
	if err != nil {
		t.Fatalf("read codex/AGENTS.md: %v", err)
	}
	agents := string(agentsBytes)
	entries, err := os.ReadDir(filepath.Join(root, "codex/instructions"))
	if err != nil {
		t.Fatalf("read codex/instructions: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.Contains(agents, e.Name()) {
			t.Errorf("codex/AGENTS.md routing lacks codex/instructions/%s", e.Name())
		}
	}
}
