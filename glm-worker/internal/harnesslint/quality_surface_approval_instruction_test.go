package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQualitySurfaceApprovalUsesDedicatedParentAction(t *testing.T) {
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
		"required_action:\"approve-surface\"",
		"glm-parent-action approve-surface --accepted-scope current-diff",
		"terminal `accept`はadmission段階でfail closed",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("glm-execution.md does not route quality-surface approval token %q", token)
		}
	}
	if strings.Contains(text, "--approval-only") {
		t.Errorf("glm-execution.md retains the removed --approval-only surface")
	}
}
