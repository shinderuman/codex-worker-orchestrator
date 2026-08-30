package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQualitySurfaceApprovalUsesApprovalOnlyParentAction(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	path := filepath.Join(root, "codex", "instructions", "glm-execution.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, token := range []string{
		"quality policy surface変更",
		"semantic fixを要求せず",
		"glm-parent-action fix <token> --accepted-scope current-diff --approval-only",
		"`--origin`を併用しない",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("glm-execution.md does not route quality-surface approval token %q", token)
		}
	}
}
