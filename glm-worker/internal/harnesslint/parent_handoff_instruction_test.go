package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalParentHandoffIsRouted(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))

	checks := []struct {
		path   string
		tokens []string
	}{
		{
			path: "codex/instructions/glm-packets.md",
			tokens: []string{
				"glm-worker --handoff",
				"required_action",
				"allowed_actions",
			},
		},
		{
			path: "codex/instructions/glm-execution.md",
			tokens: []string{
				"glm-worker --handoff",
				"required_action",
				"allowed_actions",
			},
		},
		{
			path: "codex/instructions/task-request-boundary.md",
			tokens: []string{
				"glm-worker --handoff",
				"required_action",
				"allowed_actions",
			},
		},
	}

	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.path)))
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		text := string(data)
		for _, token := range check.tokens {
			if !strings.Contains(text, token) {
				t.Errorf("%s does not route canonical parent handoff token %q", check.path, token)
			}
		}
	}
}
