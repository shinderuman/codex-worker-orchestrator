package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleUsesTaskAuthoritySnapshotAfterTaskFileRetirement(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskPath := "IMPLEMENTATION_TASKS/014-retired.md"
	for rel, content := range map[string]string{
		"IMPLEMENTATION_PLAN.local.md": "plan\n",
		"IMPLEMENTATION_RULES.md":      "rules\n",
		"IMPLEMENTATION_HISTORY.md":    "history\n",
		taskPath:                       "# Task 014\n",
	} {
		writeBundleFile(t, filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel)), content)
	}
	_, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", taskPath); err != nil {
		t.Fatal(err)
	}
	authority := []byte("# Task 014\n\nfinal authority\n")
	if err := st.SaveCurrentTaskAuthority(taskPath, authority); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, filepath.FromSlash(taskPath))); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvidenceStatus != "complete" {
		t.Fatalf("bundle incomplete: %v", output.Missing)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	repoAuthorityPath := "current-state/repository-authority/" + taskPath
	if !bytes.Equal(archive[repoAuthorityPath], authority) {
		t.Fatalf("repository authority = %q", archive[repoAuthorityPath])
	}
	if !bytes.Equal(archive["task/authority/active-task.md"], authority) {
		t.Fatalf("task authority = %q", archive["task/authority/active-task.md"])
	}
	if got := string(archive["task/authority/active-task.path"]); got != taskPath+"\n" {
		t.Fatalf("task authority path = %q", got)
	}
}
