package repositoryprojectcommit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseTaskCorpusEntryPreservesRegularFileModes(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  string
		include bool
		regular bool
	}{
		{name: "regular", record: "100644 blob deadbeef\tIMPLEMENTATION_TASKS/a.md", include: true, regular: true},
		{name: "executable", record: "100755 blob deadbeef\tIMPLEMENTATION_TASKS/b.md", include: true, regular: true},
		{name: "symlink", record: "120000 blob deadbeef\tIMPLEMENTATION_TASKS/link.md", include: true, regular: false},
		{name: "tree", record: "040000 tree deadbeef\tIMPLEMENTATION_TASKS/group.md", include: true, regular: false},
		{name: "non markdown", record: "100644 blob deadbeef\tIMPLEMENTATION_TASKS/readme.txt", include: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry, include, err := parseTaskCorpusEntry(test.record)
			if err != nil {
				t.Fatal(err)
			}
			if include != test.include {
				t.Fatalf("include = %v, want %v", include, test.include)
			}
			if include && entry.Regular != test.regular {
				t.Fatalf("regular = %v, want %v", entry.Regular, test.regular)
			}
		})
	}
}

func TestTaskCorpusEntriesPreservesCallerTreeEnumeration(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	path := filepath.Join(repo, "IMPLEMENTATION_TASKS", "group.md", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "fixture")

	controllerEntries, err := taskCorpusEntries(repo, "HEAD", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(controllerEntries) != 0 {
		t.Fatalf("controller corpus unexpectedly included tree: %#v", controllerEntries)
	}
	headEntries, err := TaskCorpusEntries(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(headEntries) != 1 || headEntries[0].Path != "IMPLEMENTATION_TASKS/group.md" || headEntries[0].Regular {
		t.Fatalf("HEAD corpus did not preserve non-regular markdown tree: %#v", headEntries)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
