package harnesslint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequirementSourceViolationsCurrentVsHistorical(t *testing.T) {
	tick := string(rune(96))
	root := t.TempDir()
	task := "IMPLEMENTATION_TASKS/child.md"
	source := "review-evidence/october-2026/request.md"
	writeTaskCorpusFile(t, root, task, fmt.Sprintf("## Resolved references\n- Historical locator: %sIMPLEMENTATION_TASKS/deleted.md%s\n- requirement-source: %sfile:%s%s\n", tick, tick, tick, source, tick))
	paths := []string{task}
	violations, err := requirementSourceViolations(root, paths)
	if err != nil || len(violations) != 1 || violations[0].Rule != requirementSourceRule ||
		!strings.Contains(violations[0].Message, source) {
		t.Fatalf("missing current source violations=%#v err=%v", violations, err)
	}
	writeTaskCorpusFile(t, root, source, "original requirement\n")
	paths = append(paths, source)
	violations, err = requirementSourceViolations(root, paths)
	if err != nil || len(violations) != 0 {
		t.Fatalf("present current source violations=%#v err=%v", violations, err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(source))); err != nil {
		t.Fatal(err)
	}
	writeTaskCorpusFile(t, root, source, "restored\n")
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(source))); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("absent", filepath.Join(root, filepath.FromSlash(source))); err != nil {
		t.Fatal(err)
	}
	violations, err = requirementSourceViolations(root, paths)
	if err != nil || len(violations) != 1 {
		t.Fatalf("symlink current source admitted: %#v, %v", violations, err)
	}
}

func TestRequirementSourceViolationsVerifyGitPinnedBlob(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, result)
		}
		return strings.TrimSpace(string(result))
	}
	git("init", "-q")
	source := "review-evidence/source.md"
	writeTaskCorpusFile(t, root, source, "original instruction\n")
	git("add", source)
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixed source")
	sha := git("rev-parse", "HEAD")
	task := "IMPLEMENTATION_TASKS/child.md"
	tick := string(rune(96))
	reference := fmt.Sprintf("## Resolved references\n- requirement-source: %sgit:%s:%s%s\n", tick, sha, source, tick)
	writeTaskCorpusFile(t, root, task, reference)
	violations, err := requirementSourceViolations(root, []string{task})
	if err != nil || len(violations) > 0 {
		t.Fatalf("valid pinned source rejected: %#v, %v", violations, err)
	}
	writeTaskCorpusFile(t, root, task, strings.ReplaceAll(reference, sha, strings.Repeat("0", 40)))
	violations, err = requirementSourceViolations(root, []string{task})
	if err != nil || len(violations) != 1 {
		t.Fatalf("missing pinned commit admitted: %#v, %v", violations, err)
	}
}
