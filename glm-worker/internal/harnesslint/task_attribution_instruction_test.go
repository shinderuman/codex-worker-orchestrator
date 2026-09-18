package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParentInstructionsFailClosedCanonicalHandoffMismatch(t *testing.T) {
	root := instructionTestRoot(t)
	for _, path := range []string{
		"codex/instructions/glm-execution.md",
		"codex/instructions/glm-packets.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		if !strings.Contains(text, "canonical handoff") || !strings.Contains(text, "consistent") || !strings.Contains(text, "推測") {
			t.Fatalf("%s does not route inconsistent canonical handoff fail-closed", path)
		}
	}
}

func TestParentPacketInstructionsBindTaskOwnerBeforeTaskStatus(t *testing.T) {
	root := instructionTestRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-packets.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, token := range []string{
		"parent_request.task_attribution",
		"task_status",
		"handover:true",
		"lifecycle_task",
		"active_task",
		"legal_next_action",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("glm-packets.md is missing ownership token %q", token)
		}
	}
}

func TestPriorityChangesRequireWholePlanAndStopBoundaryComparison(t *testing.T) {
	root := instructionTestRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "goal-development.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, token := range []string{
		"Plan全体のACTIVE / NEXT / BLOCKED順",
		"既存priority根拠",
		"停止・再開境界",
		"NEXT先頭へ割り込ませない",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("goal-development.md is missing priority token %q", token)
		}
	}
}

func instructionTestRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}
