package harnesslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type parentBehaviorEvalNoRereadRegistry struct {
	Cases []struct {
		ID       string `json:"id"`
		Positive string `json:"positive"`
		Negative string `json:"negative"`
		Evidence string `json:"evidence"`
	} `json:"cases"`
}

func TestStagedParentActionNoRereadBehaviorContract(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))

	executionData, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-execution.md"))
	if err != nil {
		t.Fatal(err)
	}
	actionSurface, ok := markdownSection(string(executionData), "## 親action surface")
	if !ok {
		t.Fatal("glm-execution.md missing parent action surface")
	}
	for _, token := range []string{
		`kind:"staged"`,
		"prepare_command",
		"machine-declared `slots`だけ",
		"staging file全体の再解釈",
		"next_command",
	} {
		if !strings.Contains(actionSurface, token) {
			t.Errorf("staged no-reread contract missing %q", token)
		}
	}

	evalData, err := os.ReadFile(filepath.Join(root, "tests", "parent-behavior-evals.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry parentBehaviorEvalNoRereadRegistry
	if err := json.Unmarshal(evalData, &registry); err != nil {
		t.Fatal(err)
	}
	var positive, negative, evidence string
	for _, item := range registry.Cases {
		if item.ID == "staged-parent-action-single-orchestration" {
			positive, negative, evidence = item.Positive, item.Negative, item.Evidence
			break
		}
	}
	if positive == "" {
		t.Fatal("staged-parent-action-single-orchestration live eval is missing")
	}
	for _, token := range []string{"one tool orchestration", "without model re-entry"} {
		if !strings.Contains(positive, token) {
			t.Errorf("positive live eval contract missing %q", token)
		}
	}
	for _, token := range []string{
		"Malformed prepare output",
		"unexpected action/path/token",
		"failed placeholder patch stops before action execution",
		"does not reread the freshly prepared staging file",
		"apply_patch",
	} {
		if !strings.Contains(negative, token) {
			t.Errorf("negative live eval contract missing %q", token)
		}
	}
	if !strings.Contains(evidence, "staging-file cat/sed read") {
		t.Error("live eval evidence no longer rejects fresh staging-file cat/sed rereads")
	}
}
