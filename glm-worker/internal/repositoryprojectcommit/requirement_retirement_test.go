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
	const source = "evidence/requirement.md"
	if err := os.Mkdir(filepath.Join(root, "evidence"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, source), []byte("immutable requirement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	git("add", source, "link.md")
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
	if _, err := makeProject(sha, source).RetireTerminalMetadata(active, false); err != nil {
		t.Fatalf("real pinned requirement source rejected: %v", err)
	}
	if _, err := makeProject(strings.Repeat("0", 40), source).RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("missing Git commit admitted by publication retirement")
	}

	fileProject := makeProject(sha, source)
	fileProject.Head = sha
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:" + source + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err != nil {
		t.Fatalf("committed current file requirement rejected: %v", err)
	}
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:missing.md" + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("missing committed current file requirement admitted")
	}
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:link.md" + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("symlinkをcommitted file sourceとして受理しました")
	}
	if _, err := makeProject(sha, "link.md").RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("symlinkを固定Git sourceとして受理しました")
	}
	if _, err := makeProject(sha, ".").RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("invalid pinned path admitted")
	}
	blob := git("rev-parse", sha+":"+source)
	if err := os.Remove(filepath.Join(root, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	if _, err := makeProject(sha, source).RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("実体が欠落したGit sourceを受理しました")
	}
	fileProject.Tasks[1].Content = []byte("## Resolved references\n- requirement-source: " + tick +
		"file:" + source + tick + "\n\n## Dependencies\nnone\n")
	if _, err := fileProject.RetireTerminalMetadata(active, false); err == nil {
		t.Fatal("実体が欠落したcommitted file sourceを受理しました")
	}
}
