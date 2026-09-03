package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var parentGitPolicyActorRe = regexp.MustCompile("親`?Codex`?|parent Codex")
var parentGitPolicyWriteRe = regexp.MustCompile("commit|push|fast-forward")
var parentGitPolicyProximityRe = regexp.MustCompile("(?:親`?Codex`?|parent Codex)[^\n]{0,12}(?:commit|push|fast-forward)|(?:commit|push|fast-forward)[^\n]{0,30}(?:親`?Codex`?|parent Codex)")

func parentGitPolicyGuardTargets(t *testing.T) []string {
	t.Helper()
	root := scenarioRepoRoot(t)
	files := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, "codex", "AGENTS.md"),
	}
	for _, dir := range []string{
		filepath.Join(root, "codex", "instructions"),
		filepath.Join(root, "codex", "glm-worker", "prompts"),
		filepath.Join(root, "codex", "rules"),
	} {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestCodexPolicySurfacesKeepParentGitWritePolicyAbsent(t *testing.T) {
	for _, path := range parentGitPolicyGuardTargets(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		fenced := false
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			if parentGitPolicyActorRe.MatchString(line) && parentGitPolicyWriteRe.MatchString(line) && parentGitPolicyProximityRe.MatchString(line) {
				t.Fatalf("親CodexのGit commit/push policy記述が残っています: %s:%d: %s", path, i+1, line)
			}
		}
	}
}
