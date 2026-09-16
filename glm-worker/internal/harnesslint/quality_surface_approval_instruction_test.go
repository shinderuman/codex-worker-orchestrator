package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQualitySurfaceApprovalUsesMachineProjectedParentAction(t *testing.T) {
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
		"quality policy surface承認",
		"action_specs",
		"required parameter",
		"semantic採否",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("glm-execution.md does not retain quality-surface semantic boundary token %q", token)
		}
	}
	for _, token := range []string{
		"--approval-only",
		"glm-parent-action approve-surface --accepted-scope current-diff",
	} {
		if strings.Contains(text, token) {
			t.Errorf("glm-execution.md reconstructs machine-owned approval command token %q", token)
		}
	}
}
