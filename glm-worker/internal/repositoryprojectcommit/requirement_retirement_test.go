package repositoryprojectcommit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedTerminalRetirementVerifiesPinnedRequirementSource(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, data)
		}
		return strings.TrimSpace(string(data))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "requirement.md"), []byte("immutable requirement"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "requirement.md")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "source")
	sha := git("rev-parse", "HEAD")
	tick := string(rune(96))
	active, next := "IMPLEMENTATION_TASKS/active.md", "IMPLEMENTATION_TASKS/next.md"
	plan := []byte("## ACTIVE\n- " + tick + active + tick + "\n\n## NEXT\n- " + tick + next + tick + "\n")
	makeProject := func(revision, path string) Project {
		return Project{RepoRoot: root, Plan: plan, Tasks: []Task{
			{Path: active, Content: []byte("## Dependencies\nnone\n")},
			{Path: next, Content: []byte("## Resolved references\n- requirement-source: " + tick +
				"git:" + revision + ":" + path + tick + "\n\n## Dependencies\nnone\n")},
		}}
	}
	if _, err := makeProject(sha, "requirement.md").RetireTerminalMetadata(active, false); err != nil {
		t.Fatalf("real pinned requirement source rejected: %v", err)
	}
	if _, err := makeProject(strings.Repeat("0", 40), "requirement.md").RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("missing Git commit admitted by publication retirement")
	}

	fileProject := makeProject(sha, "requirement.md")
	fileProject.Head = sha
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:requirement.md" + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err != nil {
		t.Fatalf("committed current file requirement rejected: %v", err)
	}
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:missing.md" + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("missing committed current file requirement admitted")
	}
	if _, err := makeProject(sha, ".").RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("invalid pinned path admitted")
	}
}
