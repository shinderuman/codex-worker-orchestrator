package harnesslint

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGLMWaitContractIsCompactAndToolOwned(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	legacyPath := filepath.Join(root, "codex", "instructions", "glm-wait.md")
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy glm-wait.md must be absent: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-execution.md"))
	if err != nil {
		t.Fatal(err)
	}
	waitSection, ok := markdownSection(string(data), "## 待機")
	if !ok {
		t.Fatal("glm-execution.md missing wait section")
	}
	for _, token := range []string{
		"tool/runtime境界へ委ねる",
		"glm-worker --handoff",
		"required_action",
		"allowed_actions",
		"glm-worker --watch",
	} {
		if !strings.Contains(waitSection, token) {
			t.Errorf("wait section missing %q", token)
		}
	}
	for _, token := range []string{
		"21600000",
		"最大待機時間",
		"functions.wait",
		"tools.write_stdin",
	} {
		if strings.Contains(waitSection, token) {
			t.Errorf("wait section retains parent timing transport %q", token)
		}
	}
}

func markdownSection(text, heading string) (string, bool) {
	start := strings.Index(text, heading+"\n")
	if start < 0 {
		return "", false
	}
	section := text[start:]
	if next := strings.Index(section[len(heading)+1:], "\n## "); next >= 0 {
		section = section[:len(heading)+1+next]
	}
	return section, true
}
