package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGLMWaitAlignsOuterAndInnerYield(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-wait.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, token := range []string{
		`// @exec: {"yield_time_ms":21600000,"max_output_tokens":1000}`,
		"tools.write_stdin",
		"yield-time_ms=21600000",
		"functions.wait",
		"clamp",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("glm-wait.md missing %q", token)
		}
	}
}
