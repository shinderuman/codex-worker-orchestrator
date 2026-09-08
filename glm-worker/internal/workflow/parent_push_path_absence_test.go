package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var parentPushExecPathPattern = regexp.MustCompile("exec\\.Command\\(\\s*\"git\"[^)]*\"push\"")

func TestProductionGoCodeHasNoGitPushExecutionPath(t *testing.T) {
	root := scenarioRepoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "glm-worker", "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if parentPushExecPathPattern.Match(data) {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			t.Fatalf("production codeがgit push実行経路を持ちます: %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkerPromptsKeepRemoteWriteProhibition(t *testing.T) {
	root := scenarioRepoRoot(t)
	workerPath := filepath.Join(root, "codex", "glm-worker", "prompts", "WORKER.md")
	workerData, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatal(err)
	}
	prohibition := false
	for _, line := range strings.Split(string(workerData), "\n") {
		if strings.Contains(line, "`git push`") && strings.Contains(line, "禁止") {
			prohibition = true
			break
		}
	}
	if !prohibition {
		t.Fatalf("WORKER promptがGLMのremote write禁止を明示していません: %s", workerPath)
	}
}
