package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIsParentManagedPathOwnsProtectionSet(t *testing.T) {
	for _, path := range []string{
		ParentRulesFile,
		ParentPlanFile,
		ParentHistoryFile,
		ParentTasksDir + "/task.md",
		ParentTasksDir + "/nested/task.md",
	} {
		if !IsParentManagedPath(path) {
			t.Fatalf("managed path %q was not recognized", path)
		}
	}
	for _, path := range []string{
		"",
		ParentTasksDir,
		ParentTasksDir + "-archive/task.md",
		"docs/task.md",
	} {
		if IsParentManagedPath(path) {
			t.Fatalf("non-managed path %q was recognized", path)
		}
	}
}

func TestCaptureParentFileStatesEnumeratesCurrentProtectionSetDeterministically(t *testing.T) {
	repo := t.TempDir()
	files := map[string]string{
		ParentRulesFile:                 "rules\n",
		ParentHistoryFile:               "history\n",
		ParentTasksDir + "/z.md":        "z\n",
		ParentTasksDir + "/nested/a.md": "a\n",
	}
	for name, content := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	states, err := CaptureParentFileStates(repo)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(states))
	for _, state := range states {
		paths = append(paths, state.Path)
	}
	want := []string{
		ParentHistoryFile,
		ParentPlanFile,
		ParentRulesFile,
		ParentTasksDir + "/nested/a.md",
		ParentTasksDir + "/z.md",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v want %v", paths, want)
	}
	if plan := FindParentFileState(states, ParentPlanFile); plan.Exists || plan.SHA256 != "" {
		t.Fatalf("missing plan state = %#v", plan)
	}
	for name := range files {
		if state := FindParentFileState(states, name); !state.Exists || state.SHA256 == "" {
			t.Fatalf("captured state for %s = %#v", name, state)
		}
	}
}

func TestCaptureRepositoryBoundarySnapshotIncludesParentAuthority(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ParentRulesFile), []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ParentTasksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ParentTasksDir, "task.md"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	boundary, err := CaptureRepositoryBoundarySnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	gitOnly, err := CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if boundary.ParentFiles == nil {
		t.Fatal("boundary snapshot has no parent authority state")
	}
	if gitOnly.ParentFiles != nil {
		t.Fatal("Git-only snapshot unexpectedly contains parent authority state")
	}
	if boundary.Head != gitOnly.Head || boundary.IndexDigest != gitOnly.IndexDigest || boundary.WorktreeDigest != gitOnly.WorktreeDigest || boundary.WorktreeDigestExcludingParent != gitOnly.WorktreeDigestExcludingParent {
		t.Fatalf("boundary Git identity differs from Git-only snapshot: boundary=%#v git=%#v", boundary, gitOnly)
	}
	if rules := FindParentFileState(*boundary.ParentFiles, ParentRulesFile); !rules.Exists || rules.SHA256 == "" {
		t.Fatalf("rules state = %#v", rules)
	}
	if task := FindParentFileState(*boundary.ParentFiles, ParentTasksDir+"/task.md"); !task.Exists || task.SHA256 == "" {
		t.Fatalf("task state = %#v", task)
	}
}
