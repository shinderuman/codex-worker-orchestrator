package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParentDecisionFixUsesFileActionSurface(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	for _, path := range []string{"codex/instructions/glm-execution.md", "codex/instructions/glm-packets.md"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, token := range []string{"--decision-file", "--fix-file"} {
			if !strings.Contains(text, token) {
				t.Errorf("%s missing %q", path, token)
			}
		}
	}
}
